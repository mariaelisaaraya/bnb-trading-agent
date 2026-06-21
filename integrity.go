package main

import (
	"crypto/sha256"
	"fmt"
	"math"
)

type TradeIntent struct {
	Token         string  `json:"token"`
	Direction     string  `json:"direction"`
	AmountUSD     float64 `json:"amount_usd"`
	ExpectedPrice float64 `json:"expected_price"`
	RegisteredAt  int64   `json:"registered_at"`
	Hash          string  `json:"hash"`
}

type DriftType string

const (
	DriftNone      DriftType = ""
	DriftToken     DriftType = "token"
	DriftDirection DriftType = "direction"
	DriftPrice     DriftType = "price"
	DriftAmount    DriftType = "amount"
)

type DriftResult struct {
	HasDrift bool
	Type     DriftType
	Expected string
	Got      string
}

func IntentKey(token, direction string) string {
	return token + ":" + direction
}

func HashIntent(token, direction string, amountUSD, price float64) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n%.4f\n%.4f", token, direction, amountUSD, price)
	return fmt.Sprintf("sha256:%x", h.Sum(nil))
}

func CheckTradeDrift(intent TradeIntent, token, direction string, amountUSD, executionPrice float64, tolerance float64) DriftResult {
	if intent.Token != token {
		return DriftResult{
			HasDrift: true,
			Type:     DriftToken,
			Expected: intent.Token,
			Got:      token,
		}
	}

	if intent.Direction != direction {
		return DriftResult{
			HasDrift: true,
			Type:     DriftDirection,
			Expected: intent.Direction,
			Got:      direction,
		}
	}

	if intent.ExpectedPrice > 0 && executionPrice > 0 {
		slippage := math.Abs(executionPrice-intent.ExpectedPrice) / intent.ExpectedPrice
		if slippage > tolerance {
			return DriftResult{
				HasDrift: true,
				Type:     DriftPrice,
				Expected: fmt.Sprintf("$%.4f", intent.ExpectedPrice),
				Got:      fmt.Sprintf("$%.4f (slippage %.2f%%)", executionPrice, slippage*100),
			}
		}
	}

	if intent.AmountUSD > 0 {
		diff := math.Abs(amountUSD-intent.AmountUSD) / intent.AmountUSD
		if diff > tolerance*5 {
			return DriftResult{
				HasDrift: true,
				Type:     DriftAmount,
				Expected: fmt.Sprintf("$%.2f", intent.AmountUSD),
				Got:      fmt.Sprintf("$%.2f", amountUSD),
			}
		}
	}

	return DriftResult{}
}
