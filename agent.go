package main

import (
	"fmt"
	"path/filepath"
	"time"
)

// Agent is the main autonomous trading loop.
// Data flow: CMC market data → strategy → trade guard → TWAK execution → audit.
type Agent struct {
	cfg       AgentConfig
	configDir string
	cmc       *CMCClient
	twak      *TWAKClient
	x402      *X402Client
}

// NewAgent creates an Agent from config. Validates required fields.
func NewAgent(cfg AgentConfig, configDir string) (*Agent, error) {
	if cfg.CMCAPIKey == "" {
		return nil, fmt.Errorf("cmc_api_key is required in config.yaml")
	}
	if cfg.TWAK.WalletAddress == "" && !cfg.TWAK.DryRun {
		return nil, fmt.Errorf("twak.wallet_address is required when dry_run is false")
	}
	twak := NewTWAKClient(cfg.TWAK)
	return &Agent{
		cfg:       cfg,
		configDir: configDir,
		cmc:       NewCMCClient(cfg.CMCAPIKey),
		twak:      twak,
		x402:      NewX402Client(cfg.X402, twak),
	}, nil
}

// Run starts the autonomous trading loop. Runs until ctx is cancelled or
// a fatal error occurs. Executes one iteration immediately, then sleeps.
func (a *Agent) Run(verbose bool) error {
	interval := time.Duration(a.cfg.TradeIntervalMinutes) * time.Minute
	if interval == 0 {
		interval = 15 * time.Minute
	}

	fmt.Printf("\nBNB Trading Agent starting\n")
	fmt.Printf("  Token:    %s\n", a.cfg.Strategy.Token)
	fmt.Printf("  Interval: %v\n", interval)
	fmt.Printf("  Mode:     %s\n", modeLabel(a.cfg.TWAK.DryRun))
	fmt.Printf("  Config:   %s\n\n", a.configDir)

	for {
		if err := a.iterate(verbose); err != nil {
			fmt.Printf("[error] %v\n", err)
		}
		fmt.Printf("\nNext evaluation in %v — press Ctrl+C to stop.\n", interval)
		time.Sleep(interval)
	}
}

// RunOnce executes a single trading iteration and returns.
// Useful for cron-based scheduling.
func (a *Agent) RunOnce(verbose bool) error {
	return a.iterate(verbose)
}

func (a *Agent) iterate(verbose bool) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")

	// Step 1: Fetch wallet balance once for all tokens.
	walletBal, err := a.twak.GetBalance()
	if err != nil {
		fmt.Printf("[warn] could not fetch portfolio balance: %v\n", err)
	}
	portfolioUSD := walletBal.TotalUSD

	if receipt, fundErr := a.x402.SelfFund(portfolioUSD); fundErr != nil {
		fmt.Printf("[warn] x402 self-fund: %v\n", fundErr)
	} else if receipt != nil {
		fmt.Printf("x402: funded %.4f %s (tx: %s)\n", receipt.Amount, receipt.Asset, receipt.TxHash)
	}

	// Step 2: Open trade guard once (shared across all tokens this cycle).
	guard, err := NewTradeGuard(a.configDir)
	if err != nil {
		return fmt.Errorf("init trade guard: %w", err)
	}
	defer guard.Close()

	// Step 3: Evaluate each token; accumulate current prices for portfolio valuation.
	tokenPrices := make(map[string]float64)
	for _, tok := range a.cfg.Strategy.ActiveTokens() {
		price := a.evaluateToken(now, tok, walletBal, guard, verbose)
		if price > 0 {
			tokenPrices[tok.Symbol] = price
		}
	}

	// Step 4: Update portfolio = liquid balance + market value of tracked holdings.
	// walletBal.TotalUSD only covers USDT + native BNB; holdings tracks BEP-20 positions.
	holdingsUSD := guard.EstimateHoldingsUSD(tokenPrices)
	totalPortfolio := walletBal.TotalUSD + holdingsUSD
	if totalPortfolio > 0 {
		guard.UpdatePortfolio(totalPortfolio)
	}

	// Step 5: Competition keep-alive — if no eligible trade in 20+ hours, execute a
	// minimal ETH buy to maintain daily participation without significant capital risk.
	if guard.HoursSinceLastTrade() >= 20.0 && walletBal.USDTUSD >= 2.0 {
		a.keepAliveBuy(now, walletBal, guard, verbose)
	}

	return nil
}

func (a *Agent) keepAliveBuy(now string, walletBal WalletBalance, guard *TradeGuard, verbose bool) {
	ethTok := TokenConfig{
		Symbol:         "ETH",
		Contract:       "0x2170ed0880ac9a755fd29b2688956bd959f933f8",
		TradeAmountUSD: 1.50,
	}
	fmt.Printf("[%s] Keep-alive: no trade in 20h — buying $1.50 ETH\n", now)

	data, err := a.cmc.FetchMarketData("ETH")
	if err != nil {
		fmt.Printf("  [keep-alive] fetch ETH price: %v\n", err)
		return
	}

	tradeID := fmt.Sprintf("keepalive_%d", time.Now().UnixNano())
	decision := TradeDecision{
		Token:     "ETH",
		Direction: "buy",
		AmountUSD: 1.50,
		Price:     data.Price,
		Reason:    "keep-alive: competition daily participation",
	}
	guardResult := guard.Run(tradeID, decision)
	fmt.Printf("  Guard:     %s [%s]\n", guardResult.Decision, joinStages(guardResult))
	if guardResult.Decision == "block" {
		fmt.Printf("  Blocked:   %s\n", guardResult.Reason)
		return
	}

	receipt, err := a.twak.ExecuteBuy(ethTok.Contract, 1.50, data.Price)
	if err != nil {
		fmt.Printf("  [keep-alive] execute: %v\n", err)
		return
	}
	// Intentionally do NOT RecordHolding here: keep-alive buys are for
	// daily competition eligibility only. Tracking the position would
	// trigger an immediate sell signal on the next cycle when F&G is low.
	fmt.Printf("  Executed:  buy $%.2f ETH @ $%.2f (tx: %s)\n",
		receipt.AmountUSD, receipt.Price, receipt.TxHash)
}

// evaluateToken fetches market data, runs the strategy, and executes a trade if signalled.
// Returns the current token price (for portfolio valuation), or 0 on error.
func (a *Agent) evaluateToken(now string, tok TokenConfig, walletBal WalletBalance, guard *TradeGuard, verbose bool) float64 {
	fmt.Printf("[%s] Evaluating %s...\n", now, tok.Symbol)

	data, err := a.cmc.FetchMarketData(tok.Symbol)
	if err != nil {
		fmt.Printf("  [error] fetch market data: %v\n", err)
		return 0
	}

	// Fetch price history and compute technical indicators.
	if prices, err := a.twak.GetPriceHistory(tok.Symbol); err == nil && len(prices) >= 30 {
		data.EMA7 = EMA(prices, 7)
		data.EMA30 = EMA(prices, 30)
		data.RSI14 = RSI(prices, 14)
	}

	if verbose {
		fmt.Printf("  Price:     $%.4f\n", data.Price)
		fmt.Printf("  24h:       %+.2f%%\n", data.Change24h)
		fmt.Printf("  7d:        %+.2f%%\n", data.Change7d)
		fmt.Printf("  F&G:       %d (%s)\n", data.FearGreedValue, data.FearGreedLabel)
		if data.EMA7 > 0 {
			trend := "↑ bull"
			if data.EMA7 < data.EMA30 {
				trend = "↓ bear"
			}
			fmt.Printf("  EMA7/30:   $%.2f / $%.2f (%s)\n", data.EMA7, data.EMA30, trend)
			fmt.Printf("  RSI-14:    %.1f\n", data.RSI14)
		}
	}

	// Use per-token trade amount if set, otherwise fall back to global.
	cfg := a.cfg.Strategy
	if tok.TradeAmountUSD > 0 {
		cfg.Token = tok.Symbol
		cfg.TokenContract = tok.Contract
		cfg.TradeAmountUSD = tok.TradeAmountUSD
	}

	signal := Evaluate(data, cfg)
	fmt.Printf("  Signal:    %s — %s\n", signal.Action, signal.Reason)

	if signal.Action == "hold" {
		return data.Price
	}

	// Adjust trade amount to available balance.
	available := signal.AmountUSD
	if signal.Action == "buy" {
		if walletBal.USDTUSD < signal.AmountUSD {
			available = walletBal.USDTUSD * 0.90
		}
		if available < 1.0 {
			fmt.Printf("  Skip:      insufficient USDT for buy (available $%.2f < $1.00)\n", available)
			return data.Price
		}
	}
	if signal.Action == "sell" {
		holdingsUSD := guard.HoldingValueUSD(tok.Symbol, data.Price)
		if holdingsUSD < 0.25 {
			fmt.Printf("  Skip:      no %s position to sell (holdings ~$%.2f)\n", tok.Symbol, holdingsUSD)
			return data.Price
		}
		if holdingsUSD < signal.AmountUSD {
			available = holdingsUSD * 0.95
		}
	}

	// Run guard pipeline.
	tradeID := fmt.Sprintf("trade_%d", time.Now().UnixNano())
	decision := TradeDecision{
		Token:     tok.Symbol,
		Direction: signal.Action,
		AmountUSD: available,
		Price:     signal.Price,
		Reason:    signal.Reason,
	}

	guardResult := guard.Run(tradeID, decision)
	fmt.Printf("  Guard:     %s [%s]\n", guardResult.Decision, joinStages(guardResult))
	if guardResult.Decision == "block" {
		fmt.Printf("  Blocked:   %s\n", guardResult.Reason)
		return data.Price
	}

	// Execute via TWAK — use contract address when available.
	twakToken := tok.Symbol
	if tok.Contract != "" {
		twakToken = tok.Contract
	}
	var receipt *TradeReceipt
	switch signal.Action {
	case "buy":
		receipt, err = a.twak.ExecuteBuy(twakToken, available, signal.Price)
	case "sell":
		receipt, err = a.twak.ExecuteSell(twakToken, available, signal.Price)
	}
	if err != nil {
		fmt.Printf("  [error] execute trade: %v\n", err)
		return data.Price
	}

	guard.RecordHolding(tok.Symbol, signal.Action, receipt.AmountUSD, receipt.Price)

	fmt.Printf("  Executed:  %s $%.2f %s @ $%.4f\n",
		receipt.Direction, receipt.AmountUSD, tok.Symbol, receipt.Price)
	fmt.Printf("  TxHash:    %s\n", receipt.TxHash)
	return data.Price
}

func modeLabel(dryRun bool) string {
	if dryRun {
		return "DRY RUN (no real trades)"
	}
	return "LIVE (real trades on BSC)"
}

func joinStages(r GuardResult) string {
	stages := r.StageStrings()
	result := ""
	for i, s := range stages {
		if i > 0 {
			result += " → "
		}
		result += s
	}
	return result
}

// PrintAudit displays the audit trail from disk.
func PrintAudit(configDir string, verify bool) error {
	auditPath := filepath.Join(configDir, "audit.jsonl")
	entries, err := ReadEntries(auditPath)
	if err != nil {
		return fmt.Errorf("read audit: %w", err)
	}

	if len(entries) == 0 {
		fmt.Println("No audit entries found.")
		return nil
	}

	fmt.Printf("\nBNB Trading Agent — Audit Trail (%d entries)\n", len(entries))
	fmt.Println("═══════════════════════════════════════════════════════")

	for _, e := range entries {
		fmt.Printf("\n  %s\n", e.Timestamp)
		fmt.Printf("  Trade:    %s %s $%.2f @ $%.4f\n", e.Direction, e.Token, e.AmountUSD, e.Price)
		fmt.Printf("  Decision: %s\n", e.Decision)
		if e.Reason != "" {
			fmt.Printf("  Reason:   %s\n", e.Reason)
		}
		fmt.Printf("  Hash:     %s\n", e.Hash[:20]+"...")
	}

	if verify {
		idx := VerifyChain(entries)
		fmt.Println()
		if idx == -1 {
			fmt.Printf("Hash chain: VALID (%d entries)\n\n", len(entries))
		} else {
			fmt.Printf("Hash chain: INVALID at entry %d\n\n", idx)
		}
	}

	return nil
}
