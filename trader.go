package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
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
	WalletAddress string `yaml:"wallet_address"`
	Password      string `yaml:"password"`  // wallet encryption password
	DryRun        bool   `yaml:"dry_run"`   // if true, log trades without executing
}

// TWAKClient wraps the TWAK CLI for local self-custody signing.
// All private keys stay on device — TWAK never sends them to a server.
type TWAKClient struct {
	cfg TWAKConfig
}

// NewTWAKClient creates a TWAK client.
func NewTWAKClient(cfg TWAKConfig) *TWAKClient {
	return &TWAKClient{cfg: cfg}
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

// GetBalance returns the USD value of the agent wallet's portfolio on BSC.
func (t *TWAKClient) GetBalance() (float64, error) {
	if t.cfg.DryRun {
		return 100.0, nil // mock $100 for dry-run
	}
	out, err := t.run("wallet", "balance", "--format", "json")
	if err != nil {
		return 0, fmt.Errorf("twak wallet balance: %w", err)
	}

	var resp struct {
		TotalUSD float64 `json:"total_usd"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		// Fallback: try to parse a plain float from output.
		var v float64
		if _, scanErr := fmt.Sscanf(strings.TrimSpace(out), "%f", &v); scanErr == nil {
			return v, nil
		}
		return 0, fmt.Errorf("parse balance response: %w", err)
	}
	return resp.TotalUSD, nil
}

// ExecuteBuy buys the given USD amount of token using TWAK's swap.
// Equivalent to: twak swap USDT <token> --usd <amount> --chain bsc --slippage 2 --password <pw> --json
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
		"--slippage", "2",
		"--password", t.cfg.Password,
		"--json",
	)
	if err != nil {
		return nil, fmt.Errorf("twak swap buy %s $%.2f: %w\n%s", token, amountUSD, err, out)
	}

	return parseTWAKReceipt(out, token, "buy", amountUSD, expectedPrice)
}

// ExecuteSell sells the given USD amount of token back to USDT using TWAK's swap.
// Equivalent to: twak swap <token> USDT --usd <amount> --chain bsc --slippage 2 --password <pw> --json
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
		"--slippage", "2",
		"--password", t.cfg.Password,
		"--json",
	)
	if err != nil {
		return nil, fmt.Errorf("twak swap sell %s $%.2f: %w\n%s", token, amountUSD, err, out)
	}

	return parseTWAKReceipt(out, token, "sell", amountUSD, expectedPrice)
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

func parseTWAKReceipt(raw, token, direction string, amountUSD, expectedPrice float64) (*TradeReceipt, error) {
	var resp struct {
		TxHash    string  `json:"tx_hash"`
		Price     float64 `json:"execution_price"`
		GasUSD    float64 `json:"gas_usd"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		// TWAK might print the tx hash on a single line — handle gracefully.
		txHash := strings.TrimSpace(raw)
		return &TradeReceipt{
			TxHash:    txHash,
			Token:     token,
			Direction: direction,
			AmountUSD: amountUSD,
			Price:     expectedPrice,
			Timestamp: time.Now(),
		}, nil
	}

	price := resp.Price
	if price == 0 {
		price = expectedPrice
	}

	return &TradeReceipt{
		TxHash:    resp.TxHash,
		Token:     token,
		Direction: direction,
		AmountUSD: amountUSD,
		Price:     price,
		GasUSD:    resp.GasUSD,
		Timestamp: time.Now(),
	}, nil
}
