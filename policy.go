package main

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type TradingPolicy struct {
	MaxTradeUSD       float64  `yaml:"max_trade_usd"`
	DailyLossCapUSD   float64  `yaml:"daily_loss_cap_usd"`
	DrawdownCap       float64  `yaml:"drawdown_cap"`
	MaxTradesPerHour  int      `yaml:"max_trades_per_hour"`
	SlippageTolerance float64  `yaml:"slippage_tolerance"`
	AllowedTokens     []string `yaml:"allowed_tokens"`
}

type PolicyDecision string

const (
	DecisionAllow           PolicyDecision = "allow"
	DecisionAmountExceeded  PolicyDecision = "amount_exceeded"
	DecisionRateLimited     PolicyDecision = "rate_limited"
	DecisionDailyCapHit     PolicyDecision = "daily_loss_cap"
	DecisionDrawdownHit     PolicyDecision = "drawdown_cap"
	DecisionTokenNotAllowed PolicyDecision = "token_not_allowed"
)

type PolicyResult struct {
	Allowed  bool
	Decision PolicyDecision
	Reason   string
}

type SpendRecord struct {
	Amount float64 `json:"amount"`
	At     int64   `json:"at"`
}

func DefaultPolicy() TradingPolicy {
	return TradingPolicy{
		MaxTradeUSD:      200.00,
		DailyLossCapUSD:  150.00,
		DrawdownCap:      0.25,
		MaxTradesPerHour: 4,
		SlippageTolerance: 0.02,
		AllowedTokens: []string{
			"BNB", "CAKE", "USDT", "USDC", "BUSD", "ETH",
			"BTCB", "ADA", "DOT", "LINK", "UNI", "AAVE",
		},
	}
}

func LoadPolicy(path string) (TradingPolicy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultPolicy(), nil
		}
		return TradingPolicy{}, fmt.Errorf("read policy: %w", err)
	}
	p := DefaultPolicy()
	if err := yaml.Unmarshal(data, &p); err != nil {
		return TradingPolicy{}, fmt.Errorf("parse policy: %w", err)
	}
	return p, nil
}

func SavePolicy(path string, p TradingPolicy) error {
	data, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal policy: %w", err)
	}
	header := []byte("# BNB Trading Agent risk policy\n# BNB Hack 2026 — edit to tune risk limits\n\n")
	return os.WriteFile(path, append(header, data...), 0600)
}

func CheckPolicy(p TradingPolicy, token string, amountUSD float64, calls []int64, spends []SpendRecord, portfolioValue, peakValue float64) PolicyResult {
	now := time.Now()

	if len(p.AllowedTokens) > 0 && !tokenAllowed(token, p.AllowedTokens) {
		return PolicyResult{
			Decision: DecisionTokenNotAllowed,
			Reason:   fmt.Sprintf("token %s is not on the allowed list", token),
		}
	}

	if p.MaxTradeUSD > 0 && amountUSD > p.MaxTradeUSD {
		return PolicyResult{
			Decision: DecisionAmountExceeded,
			Reason:   fmt.Sprintf("trade size $%.2f exceeds per-trade limit $%.2f", amountUSD, p.MaxTradeUSD),
		}
	}

	if p.MaxTradesPerHour > 0 {
		cutoff := now.Add(-1 * time.Hour).Unix()
		recent := 0
		for _, ts := range calls {
			if ts > cutoff {
				recent++
			}
		}
		if recent >= p.MaxTradesPerHour {
			return PolicyResult{
				Decision: DecisionRateLimited,
				Reason:   fmt.Sprintf("rate limit: %d trades already executed this hour", p.MaxTradesPerHour),
			}
		}
	}

	if p.DailyLossCapUSD > 0 {
		cutoff := now.Add(-24 * time.Hour).Unix()
		var totalSpent float64
		for _, s := range spends {
			if s.At > cutoff {
				totalSpent += s.Amount
			}
		}
		if totalSpent+amountUSD > p.DailyLossCapUSD {
			return PolicyResult{
				Decision: DecisionDailyCapHit,
				Reason:   fmt.Sprintf("daily loss cap $%.2f reached", p.DailyLossCapUSD),
			}
		}
	}

	// Stop trading if portfolio dropped more than DrawdownCap from peak.
	// Competition disqualifies at 30% — cap is set at 25% for safety buffer.
	if p.DrawdownCap > 0 && peakValue > 0 && portfolioValue > 0 {
		drawdown := (peakValue - portfolioValue) / peakValue
		if drawdown >= p.DrawdownCap {
			return PolicyResult{
				Decision: DecisionDrawdownHit,
				Reason:   fmt.Sprintf("drawdown %.1f%% exceeds cap %.1f%%", drawdown*100, p.DrawdownCap*100),
			}
		}
	}

	return PolicyResult{Allowed: true, Decision: DecisionAllow}
}

func tokenAllowed(token string, allowed []string) bool {
	for _, a := range allowed {
		if a == token {
			return true
		}
	}
	return false
}
