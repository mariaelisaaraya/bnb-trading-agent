package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// TradeReceipt is the confirmed result of an executed trade.
type TradeReceipt struct {
	TxHash    string
	Token     string
	Direction string
	AmountUSD float64
	Price     float64
	GasUSD    float64
	Timestamp time.Time
}

// TWAKConfig holds Trust Wallet Agent Kit connection settings.
type TWAKConfig struct {
	WalletAddress string  `yaml:"wallet_address"`
	Password      string  `yaml:"password"`     // wallet encryption password; prefer TWAK_PASSWORD env var
	DryRun        bool    `yaml:"dry_run"`      // if true, log trades without executing
	SlippagePct   float64 `yaml:"slippage_pct"` // swap slippage tolerance in percent (default 5)
}

// TWAKClient wraps the TWAK CLI for local self-custody signing.
// All private keys stay on device — TWAK never sends them to a server.
type TWAKClient struct {
	cfg TWAKConfig
}

// NewTWAKClient creates a TWAK client. The wallet password can be supplied
// via the TWAK_PASSWORD env var so it never has to live in config.yaml.
func NewTWAKClient(cfg TWAKConfig) *TWAKClient {
	if env := os.Getenv("TWAK_PASSWORD"); env != "" {
		cfg.Password = env
	}
	if cfg.SlippagePct <= 0 {
		cfg.SlippagePct = 5
	}
	return &TWAKClient{cfg: cfg}
}

// slippageArg formats the configured slippage for the twak CLI.
func (t *TWAKClient) slippageArg() string {
	return strconv.FormatFloat(t.cfg.SlippagePct, 'f', -1, 64)
}

// Register registers the agent wallet in the BSC competition contract.
// Equivalent to: twak compete register
func (t *TWAKClient) Register() error {
	if t.cfg.DryRun {
		fmt.Println("[dry-run] twak compete register")
		return nil
	}
	out, err := t.run("compete", "register")
	if err != nil {
		return fmt.Errorf("twak compete register: %w\n%s", err, out)
	}
	fmt.Printf("Registered on BSC competition contract:\n%s\n", out)
	return nil
}

// WalletBalance holds parsed BSC wallet balances.
type WalletBalance struct {
	TotalUSD float64
	BNBUSD   float64 // native BNB value in USD
	USDTUSD  float64 // USDT token balance (1:1 USD)
}

// GetBalance returns the detailed BSC wallet balance.
func (t *TWAKClient) GetBalance() (WalletBalance, error) {
	if t.cfg.DryRun {
		return WalletBalance{TotalUSD: 100, BNBUSD: 50, USDTUSD: 50}, nil
	}
	out, err := t.run("wallet", "balance", "--chain", "bsc", "--json")
	if err != nil {
		return WalletBalance{}, fmt.Errorf("twak wallet balance: %w", err)
	}

	// twak may emit a non-JSON warning line before the JSON object; find the first '{'.
	jsonStart := strings.Index(out, "{")
	if jsonStart > 0 {
		out = out[jsonStart:]
	}

	var resp struct {
		TotalUSD float64 `json:"totalUsd"`
		Tokens   []struct {
			Symbol  string `json:"symbol"`
			Balance string `json:"balance"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return WalletBalance{}, fmt.Errorf("parse balance response: %w", err)
	}

	// resp.TotalUSD from twak = native BNB value only (excludes ERC20 tokens).
	bal := WalletBalance{BNBUSD: resp.TotalUSD}
	for _, tok := range resp.Tokens {
		if tok.Symbol == "USDT" {
			fmt.Sscanf(tok.Balance, "%f", &bal.USDTUSD)
		}
	}
	bal.TotalUSD = bal.BNBUSD + bal.USDTUSD
	return bal, nil
}

// ExecuteBuy buys the given USD amount of token using TWAK's swap.
// Equivalent to: twak swap USDT <token> --usd <amount> --chain bsc --slippage <pct> --password <pw> --json
func (t *TWAKClient) ExecuteBuy(token string, amountUSD float64, expectedPrice float64) (*TradeReceipt, error) {
	if t.cfg.DryRun {
		return &TradeReceipt{
			TxHash:    "0xDRYRUN_BUY_" + token,
			Token:     token,
			Direction: "buy",
			AmountUSD: amountUSD,
			Price:     expectedPrice,
			GasUSD:    0.05,
			Timestamp: time.Now(),
		}, nil
	}

	out, err := t.run(
		"swap", "USDT", token,
		"--usd", fmt.Sprintf("%.2f", amountUSD),
		"--chain", "bsc",
		"--slippage", t.slippageArg(),
		"--password", t.cfg.Password,
		"--json",
	)
	if err != nil {
		return nil, fmt.Errorf("twak swap buy %s $%.2f: %w\n%s", token, amountUSD, err, out)
	}

	return parseTWAKReceipt(out, token, "buy", amountUSD, expectedPrice)
}

// ExecuteSell sells the given USD amount of token back to USDT using TWAK's swap.
// Equivalent to: twak swap <token> USDT --usd <amount> --chain bsc --slippage <pct> --password <pw> --json
func (t *TWAKClient) ExecuteSell(token string, amountUSD float64, expectedPrice float64) (*TradeReceipt, error) {
	if t.cfg.DryRun {
		return &TradeReceipt{
			TxHash:    "0xDRYRUN_SELL_" + token,
			Token:     token,
			Direction: "sell",
			AmountUSD: amountUSD,
			Price:     expectedPrice,
			GasUSD:    0.05,
			Timestamp: time.Now(),
		}, nil
	}

	out, err := t.run(
		"swap", token, "USDT",
		"--usd", fmt.Sprintf("%.2f", amountUSD),
		"--chain", "bsc",
		"--slippage", t.slippageArg(),
		"--password", t.cfg.Password,
		"--json",
	)
	if err != nil {
		return nil, fmt.Errorf("twak swap sell %s $%.2f: %w\n%s", token, amountUSD, err, out)
	}

	return parseTWAKReceipt(out, token, "sell", amountUSD, expectedPrice)
}

// GetPriceHistory returns closing prices for the past week via TWAK.
// Prices are in chronological order (oldest first).
func (t *TWAKClient) GetPriceHistory(symbol string) ([]float64, error) {
	if t.cfg.DryRun {
		prices := make([]float64, 57)
		for i := range prices {
			prices[i] = 100.0
		}
		return prices, nil
	}

	out, err := t.run("price", symbol, "--history", "week", "--json")
	if err != nil {
		return nil, fmt.Errorf("twak price history %s: %w", symbol, err)
	}

	// TWAK may emit a non-JSON prefix; find the first '{'.
	if idx := strings.Index(out, "{"); idx > 0 {
		out = out[idx:]
	}

	var resp struct {
		History []struct {
			Price float64 `json:"price"`
		} `json:"history"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return nil, fmt.Errorf("parse price history: %w", err)
	}

	prices := make([]float64, len(resp.History))
	for i, h := range resp.History {
		prices[i] = h.Price
	}
	return prices, nil
}

func (t *TWAKClient) run(args ...string) (string, error) {
	cmd := exec.Command("twak", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		combined := stdout.String() + stderr.String()
		return combined, err
	}
	return stdout.String(), nil
}

var txHashPattern = regexp.MustCompile(`0x[0-9a-fA-F]{64}`)

func parseTWAKReceipt(raw, token, direction string, amountUSD, expectedPrice float64) (*TradeReceipt, error) {
	// twak prints human-readable progress lines before the JSON receipt.
	jsonPart := raw
	if idx := strings.Index(jsonPart, "{"); idx > 0 {
		jsonPart = jsonPart[idx:]
	}

	var resp struct {
		TxHash string  `json:"tx_hash"`
		Hash   string  `json:"hash"` // twak >= 0.19 emits "hash"
		Price  float64 `json:"execution_price"`
		GasUSD float64 `json:"gas_usd"`
	}
	if err := json.Unmarshal([]byte(jsonPart), &resp); err != nil {
		// No JSON receipt — salvage a tx hash from the raw output if present.
		return &TradeReceipt{
			TxHash:    txHashPattern.FindString(raw),
			Token:     token,
			Direction: direction,
			AmountUSD: amountUSD,
			Price:     expectedPrice,
			Timestamp: time.Now(),
		}, nil
	}

	txHash := resp.TxHash
	if txHash == "" {
		txHash = resp.Hash
	}
	if txHash == "" {
		txHash = txHashPattern.FindString(raw)
	}

	price := resp.Price
	if price == 0 {
		price = expectedPrice
	}

	return &TradeReceipt{
		TxHash:    txHash,
		Token:     token,
		Direction: direction,
		AmountUSD: amountUSD,
		Price:     price,
		GasUSD:    resp.GasUSD,
		Timestamp: time.Now(),
	}, nil
}
