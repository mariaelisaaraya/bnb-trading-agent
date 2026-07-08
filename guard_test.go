package main

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestGuard(t *testing.T) (*TradeGuard, string) {
	t.Helper()
	dir := t.TempDir()
	if err := EnsureConfigDir(dir); err != nil {
		t.Fatalf("ensure config dir: %v", err)
	}
	guard, err := NewTradeGuard(dir)
	if err != nil {
		t.Fatalf("new guard: %v", err)
	}
	t.Cleanup(guard.Close)
	return guard, dir
}

func testDecision() TradeDecision {
	return TradeDecision{
		Token: "BNB", Direction: "buy",
		AmountUSD: 50, Price: 650,
		Reason: "test",
	}
}

func TestGuardAllowsNormalTrade(t *testing.T) {
	guard, _ := newTestGuard(t)
	result := guard.Run("t1", testDecision())
	if result.Decision != "allow" {
		t.Fatalf("expected allow, got %s (%s)", result.Decision, result.Reason)
	}
}

func TestCircuitBreakerBlocksAllTrades(t *testing.T) {
	guard, dir := newTestGuard(t)
	haltPath := filepath.Join(dir, HaltFileName)
	if err := os.WriteFile(haltPath, []byte("test\n"), 0600); err != nil {
		t.Fatalf("write halt file: %v", err)
	}

	result := guard.Run("t1", testDecision())
	if result.Decision != "block" {
		t.Fatalf("expected block while halted, got %s", result.Decision)
	}
	if len(result.Stages) == 0 || result.Stages[0].Name != "halt" {
		t.Fatalf("expected halt stage first, got %+v", result.Stages)
	}

	if err := os.Remove(haltPath); err != nil {
		t.Fatalf("remove halt file: %v", err)
	}
	result = guard.Run("t2", testDecision())
	if result.Decision != "allow" {
		t.Fatalf("expected allow after resume, got %s (%s)", result.Decision, result.Reason)
	}
}

// Regression: every token in the multi-token list must be evaluated against
// its own symbol, even when it has no per-token trade amount configured.
func TestEvaluateUsesPerTokenSymbol(t *testing.T) {
	cfg := DefaultStrategyConfig() // legacy token: BNB
	tok := TokenConfig{Symbol: "CAKE", Contract: "0xcake"}

	// Mirror the assignment logic in Agent.evaluateToken.
	cfg.Token = tok.Symbol
	cfg.TokenContract = tok.Contract
	if tok.TradeAmountUSD > 0 {
		cfg.TradeAmountUSD = tok.TradeAmountUSD
	}

	data := &MarketData{Symbol: "CAKE", Price: 2.5, FearGreedValue: 70, Change24h: 2.0, Change7d: 5.0}
	sig := Evaluate(data, cfg)
	if sig.Action == "hold" && sig.Reason != "" &&
		sig.Reason[:6] == "market" {
		t.Fatalf("symbol mismatch hold — per-token symbol not applied: %s", sig.Reason)
	}
	if sig.Action != "buy" {
		t.Fatalf("expected buy signal for bullish CAKE data, got %s (%s)", sig.Action, sig.Reason)
	}
}
