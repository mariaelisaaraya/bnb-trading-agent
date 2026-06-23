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
	fmt.Printf("[%s] Evaluating %s...\n", now, a.cfg.Strategy.Token)

	// Step 1: Fetch market data.
	data, err := a.cmc.FetchMarketData(a.cfg.Strategy.Token)
	if err != nil {
		return fmt.Errorf("fetch market data: %w", err)
	}

	if verbose {
		fmt.Printf("  Price:     $%.4f\n", data.Price)
		fmt.Printf("  24h:       %+.2f%%\n", data.Change24h)
		fmt.Printf("  7d:        %+.2f%%\n", data.Change7d)
		fmt.Printf("  F&G:       %d (%s)\n", data.FearGreedValue, data.FearGreedLabel)
	}

	// Step 2: Run strategy.
	signal := Evaluate(data, a.cfg.Strategy)
	fmt.Printf("  Signal:    %s — %s\n", signal.Action, signal.Reason)

	if signal.Action == "hold" {
		return nil
	}

	// Step 3: Update portfolio value and x402 self-fund if balance is low.
	walletBal, err := a.twak.GetBalance()
	if err != nil {
		fmt.Printf("  [warn] could not fetch portfolio balance: %v — using last known\n", err)
	}
	portfolioUSD := walletBal.TotalUSD
	if receipt, fundErr := a.x402.SelfFund(portfolioUSD); fundErr != nil {
		fmt.Printf("  [warn] x402 self-fund: %v\n", fundErr)
	} else if receipt != nil {
		fmt.Printf("  x402:      funded %.4f %s (tx: %s)\n", receipt.Amount, receipt.Asset, receipt.TxHash)
	}

	// Adjust trade amount to what's actually available (leave 10% for gas/slippage).
	available := signal.AmountUSD
	if signal.Action == "sell" {
		if walletBal.BNBUSD < signal.AmountUSD {
			available = walletBal.BNBUSD * 0.90
		}
	} else if signal.Action == "buy" {
		if walletBal.USDTUSD < signal.AmountUSD {
			available = walletBal.USDTUSD * 0.90
		}
	}
	if available < 1.0 {
		fmt.Printf("  Skip:      insufficient balance for %s (available $%.2f < $1.00)\n",
			signal.Action, available)
		return nil
	}

	// Step 4: Run trade guard pipeline.
	tradeID := fmt.Sprintf("trade_%d", time.Now().UnixNano())
	decision := TradeDecision{
		Token:     signal.Token,
		Direction: signal.Action,
		AmountUSD: available,
		Price:     signal.Price,
		Reason:    signal.Reason,
	}

	guard, err := NewTradeGuard(a.configDir)
	if err != nil {
		return fmt.Errorf("init trade guard: %w", err)
	}
	defer guard.Close()

	if portfolioUSD > 0 {
		guard.UpdatePortfolio(portfolioUSD)
	}

	guardResult := guard.Run(tradeID, decision)
	fmt.Printf("  Guard:     %s [%s]\n", guardResult.Decision, joinStages(guardResult))

	if guardResult.Decision == "block" {
		fmt.Printf("  Blocked:   %s\n", guardResult.Reason)
		return nil
	}

	// Step 5: Execute trade via TWAK.
	var receipt *TradeReceipt
	switch signal.Action {
	case "buy":
		receipt, err = a.twak.ExecuteBuy(signal.Token, signal.AmountUSD, signal.Price)
	case "sell":
		receipt, err = a.twak.ExecuteSell(signal.Token, signal.AmountUSD, signal.Price)
	}
	if err != nil {
		return fmt.Errorf("execute trade: %w", err)
	}

	fmt.Printf("  Executed:  %s $%.2f %s @ $%.4f\n",
		receipt.Direction, receipt.AmountUSD, receipt.Token, receipt.Price)
	fmt.Printf("  TxHash:    %s\n", receipt.TxHash)
	fmt.Printf("  Gas:       $%.4f\n", receipt.GasUSD)

	return nil
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
