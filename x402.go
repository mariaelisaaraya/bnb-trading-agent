package main

import (
	"encoding/json"
	"fmt"
)

type X402Config struct {
	Enabled        bool    `yaml:"enabled"`
	ServiceURL     string  `yaml:"service_url"`
	PaymentAsset   string  `yaml:"payment_asset"`
	MaxPaymentUSDC float64 `yaml:"max_payment_usdc"`
	MinBalanceUSD  float64 `yaml:"min_balance_usd"`
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

// Request calls an x402-protected endpoint using TWAK's autonomous payment flow.
// The agent pays max_payment_usdc automatically if the server returns HTTP 402.
// Command: twak x402 request <url> --max-payment <atomic> --prefer-asset <asset> --prefer-network bsc --yes --auto-approve
func (x *X402Client) Request(serviceURL string) (*X402Receipt, error) {
	// USDT/USDC have 6 decimals on BSC: $0.001 = 1000 atomic units
	maxPaymentAtomic := int64(x.cfg.MaxPaymentUSDC * 1_000_000)

	if x.twak.cfg.DryRun {
		fmt.Printf("[dry-run] twak x402 request %s --max-payment %d --prefer-asset %s --prefer-network bsc --yes --auto-approve\n",
			serviceURL, maxPaymentAtomic, x.cfg.PaymentAsset)
		return &X402Receipt{
			TxHash:  "0xDRYRUN_X402",
			Amount:  x.cfg.MaxPaymentUSDC,
			Asset:   x.cfg.PaymentAsset,
			Service: serviceURL,
		}, nil
	}

	out, err := x.twak.run(
		"x402", "request",
		serviceURL,
		"--max-payment", fmt.Sprintf("%d", maxPaymentAtomic),
		"--prefer-asset", x.cfg.PaymentAsset,
		"--prefer-network", "bsc",
		"--yes",
		"--auto-approve",
	)
	if err != nil {
		return nil, fmt.Errorf("x402 request: %w\n%s", err, out)
	}

	var receipt X402Receipt
	if err := json.Unmarshal([]byte(out), &receipt); err != nil {
		return &X402Receipt{
			TxHash:  out,
			Amount:  x.cfg.MaxPaymentUSDC,
			Asset:   x.cfg.PaymentAsset,
			Service: serviceURL,
		}, nil
	}
	receipt.Service = serviceURL
	return &receipt, nil
}

// SelfFund triggers an x402 request when the portfolio balance drops below
// MinBalanceUSD. The agent pays for its own operational data without human intervention.
func (x *X402Client) SelfFund(currentUSD float64) (*X402Receipt, error) {
	if !x.cfg.Enabled {
		return nil, nil
	}
	if currentUSD >= x.cfg.MinBalanceUSD {
		return nil, nil
	}
	fmt.Printf("  x402:      balance $%.2f below $%.2f — requesting service autonomously\n",
		currentUSD, x.cfg.MinBalanceUSD)
	return x.Request(x.cfg.ServiceURL)
}
