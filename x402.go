package main

import (
	"encoding/json"
	"fmt"
)

type X402Config struct {
	Enabled       bool    `yaml:"enabled"`
	ServiceURL    string  `yaml:"service_url"`
	PaymentAsset  string  `yaml:"payment_asset"`
	PaymentAmount float64 `yaml:"payment_amount"`
	MinBalanceUSD float64 `yaml:"min_balance_usd"`
}

type X402Receipt struct {
	TxHash  string  `json:"tx_hash"`
	Amount  float64 `json:"amount"`
	Asset   string  `json:"asset"`
	Service string  `json:"service_url"`
}

type X402Client struct {
	cfg  X402Config
	twak *TWAKClient
}

func NewX402Client(cfg X402Config, twak *TWAKClient) *X402Client {
	return &X402Client{cfg: cfg, twak: twak}
}

// Pay sends an autonomous x402 micropayment for a paid service endpoint.
// The agent calls this without human intervention — funds come from the
// agent's own wallet, signed locally by TWAK.
func (x *X402Client) Pay(serviceURL string, amount float64, asset string) (*X402Receipt, error) {
	if x.twak.cfg.DryRun {
		fmt.Printf("[dry-run] twak x402 pay --url %s --amount %.4f --asset %s --chain bsc\n",
			serviceURL, amount, asset)
		return &X402Receipt{
			TxHash:  "0xDRYRUN_X402",
			Amount:  amount,
			Asset:   asset,
			Service: serviceURL,
		}, nil
	}

	out, err := x.twak.run(
		"x402", "pay",
		"--url", serviceURL,
		"--amount", fmt.Sprintf("%.4f", amount),
		"--asset", asset,
		"--chain", "bsc",
		"--json",
	)
	if err != nil {
		return nil, fmt.Errorf("x402 pay: %w\n%s", err, out)
	}

	var receipt X402Receipt
	if err := json.Unmarshal([]byte(out), &receipt); err != nil {
		return &X402Receipt{TxHash: out, Amount: amount, Asset: asset, Service: serviceURL}, nil
	}
	receipt.Service = serviceURL
	return &receipt, nil
}

// SelfFund triggers an x402 payment when the portfolio balance drops below
// MinBalanceUSD. Keeps the agent operational without manual top-ups.
func (x *X402Client) SelfFund(currentUSD float64) (*X402Receipt, error) {
	if !x.cfg.Enabled {
		return nil, nil
	}
	if currentUSD >= x.cfg.MinBalanceUSD {
		return nil, nil
	}
	fmt.Printf("  x402:      balance $%.2f below threshold $%.2f — self-funding\n",
		currentUSD, x.cfg.MinBalanceUSD)
	return x.Pay(x.cfg.ServiceURL, x.cfg.PaymentAmount, x.cfg.PaymentAsset)
}
