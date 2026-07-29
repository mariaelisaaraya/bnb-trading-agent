package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func neutralMarket(symbol string, price float64) *MarketData {
	// Neutral sentiment so no market-signal sell/buy fires and position
	// exits are the only active rules.
	return &MarketData{
		Symbol: symbol, Price: price,
		FearGreedValue: 50, FearGreedLabel: "Neutral",
		Change24h: 0.0, Change7d: 0.0,
	}
}

func TestStopLossSellsFullPosition(t *testing.T) {
	cfg := DefaultStrategyConfig()
	cfg.Token = "ETH"
	pos := PositionState{Qty: 0.01, AvgEntry: 2000, PeakPrice: 2000}

	// Price 10% below entry — beyond the default 8% stop.
	sig := Evaluate(neutralMarket("ETH", 1800), cfg, pos)
	if sig.Action != "sell" || !strings.HasPrefix(sig.Reason, "stop-loss") {
		t.Fatalf("expected stop-loss sell, got %s (%s)", sig.Action, sig.Reason)
	}
	if sig.AmountUSD != 0.01*1800 {
		t.Fatalf("expected full-position exit $%.2f, got $%.2f", 0.01*1800, sig.AmountUSD)
	}
}

func TestTakeProfitSells(t *testing.T) {
	cfg := DefaultStrategyConfig()
	cfg.Token = "ETH"
	pos := PositionState{Qty: 0.01, AvgEntry: 2000, PeakPrice: 2300}

	// +15% beats the default +12% take-profit.
	sig := Evaluate(neutralMarket("ETH", 2300), cfg, pos)
	if sig.Action != "sell" || !strings.HasPrefix(sig.Reason, "take-profit") {
		t.Fatalf("expected take-profit sell, got %s (%s)", sig.Action, sig.Reason)
	}
}

func TestTrailingStopSellsAfterGiveBack(t *testing.T) {
	cfg := DefaultStrategyConfig()
	cfg.Token = "ETH"
	// Peak was +10% over entry (arms the 5% activation); price gave back 4%
	// from peak (beyond the 3% trail) but is still above entry, so neither
	// stop-loss nor take-profit applies.
	pos := PositionState{Qty: 0.01, AvgEntry: 2000, PeakPrice: 2200}

	sig := Evaluate(neutralMarket("ETH", 2112), cfg, pos)
	if sig.Action != "sell" || !strings.HasPrefix(sig.Reason, "trailing stop") {
		t.Fatalf("expected trailing-stop sell, got %s (%s)", sig.Action, sig.Reason)
	}
}

func TestNoExitInsideBands(t *testing.T) {
	cfg := DefaultStrategyConfig()
	cfg.Token = "ETH"
	pos := PositionState{Qty: 0.01, AvgEntry: 2000, PeakPrice: 2050}

	// -2.5% PnL: inside stop-loss band, trailing not armed.
	sig := Evaluate(neutralMarket("ETH", 1950), cfg, pos)
	if sig.Action != "hold" {
		t.Fatalf("expected hold inside exit bands, got %s (%s)", sig.Action, sig.Reason)
	}
}

func TestExitRulesCanBeDisabled(t *testing.T) {
	cfg := DefaultStrategyConfig()
	cfg.Token = "ETH"
	cfg.StopLossPct = -1
	pos := PositionState{Qty: 0.01, AvgEntry: 2000, PeakPrice: 2000}

	sig := Evaluate(neutralMarket("ETH", 1500), cfg, pos) // -25%
	if strings.HasPrefix(sig.Reason, "stop-loss") {
		t.Fatalf("stop-loss fired while disabled: %s", sig.Reason)
	}
}

func TestCooldownSuppressesRebuy(t *testing.T) {
	cfg := DefaultStrategyConfig()
	cfg.Token = "CAKE"
	// Bullish data that produces a buy signal with no position.
	data := &MarketData{Symbol: "CAKE", Price: 2.5, FearGreedValue: 70, Change24h: 2.0, Change7d: 5.0}

	pos := PositionState{LastSellAt: time.Now().Add(-30 * time.Minute).Unix()}
	sig := Evaluate(data, cfg, pos)
	if sig.Action != "hold" || !strings.HasPrefix(sig.Reason, "cooldown") {
		t.Fatalf("expected cooldown hold, got %s (%s)", sig.Action, sig.Reason)
	}

	pos.LastSellAt = time.Now().Add(-5 * time.Hour).Unix() // past default 240m
	sig = Evaluate(data, cfg, pos)
	if sig.Action != "buy" {
		t.Fatalf("expected buy after cooldown expiry, got %s (%s)", sig.Action, sig.Reason)
	}
}

func TestDrawdownCapBlocksBuysNotSells(t *testing.T) {
	p := DefaultPolicy()
	p.DrawdownCap = 0.15

	// Portfolio 20% below peak.
	buy := CheckPolicy(p, "ETH", "buy", 4, nil, nil, 80, 100)
	if buy.Allowed || buy.Decision != DecisionDrawdownHit {
		t.Fatalf("expected drawdown block for buy, got %+v", buy)
	}

	sell := CheckPolicy(p, "ETH", "sell", 4, nil, nil, 80, 100)
	if !sell.Allowed {
		t.Fatalf("de-risking sell must pass under drawdown, got %+v", sell)
	}
}

func TestDailySpendCapBlocksBuysNotSells(t *testing.T) {
	p := DefaultPolicy()
	p.DailyLossCapUSD = 8
	spends := []SpendRecord{{Amount: 8, At: time.Now().Unix()}}

	buy := CheckPolicy(p, "ETH", "buy", 4, nil, spends, 100, 100)
	if buy.Allowed || buy.Decision != DecisionDailyCapHit {
		t.Fatalf("expected daily cap block for buy, got %+v", buy)
	}

	sell := CheckPolicy(p, "ETH", "sell", 4, nil, spends, 100, 100)
	if !sell.Allowed {
		t.Fatalf("sell must not count against daily spend cap, got %+v", sell)
	}
}

func TestStateMigratesLegacyHoldings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	legacy := `{"calls":{},"spends":{},"intents":{},"holdings":{"ETH":0.002},"peak_portfolio_usd":18,"current_portfolio_usd":14}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}

	s, err := LoadState(path)
	if err != nil {
		t.Fatalf("load legacy state: %v", err)
	}
	p := s.Positions["ETH"]
	if p == nil || p.Qty != 0.002 {
		t.Fatalf("legacy holdings not migrated: %+v", s.Positions)
	}
	if p.AvgEntry != 0 {
		t.Fatalf("migrated position must have unknown basis, got %f", p.AvgEntry)
	}

	// First observed price becomes the basis and peak.
	s.ObservePrice("ETH", 1900)
	if p.AvgEntry != 1900 || p.PeakPrice != 1900 {
		t.Fatalf("ObservePrice did not adopt basis: %+v", p)
	}

	// Round-trip: saved state must not resurrect the legacy field.
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["holdings"]; ok {
		t.Fatalf("legacy holdings field persisted after migration: %s", raw)
	}
}

func TestRecordBuyAveragesEntryAndSellClearsOnFullExit(t *testing.T) {
	s := NewState()
	s.RecordBuy("ETH", 100, 2000) // 0.05 @ 2000
	s.RecordBuy("ETH", 100, 1000) // 0.10 @ 1000 → 0.15 total, $200 cost

	p := s.Positions["ETH"]
	wantAvg := 200.0 / 0.15
	if diff := p.AvgEntry - wantAvg; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("avg entry = %f, want %f", p.AvgEntry, wantAvg)
	}

	s.RecordSell("ETH", 0.15*1500, 1500) // full exit
	if p.Qty != 0 || p.AvgEntry != 0 || p.PeakPrice != 0 {
		t.Fatalf("full exit must reset position, got %+v", p)
	}
	if p.LastSellAt == 0 {
		t.Fatal("full exit must stamp LastSellAt for cooldown")
	}
}

func TestRollingPeakIgnoresStaleHighs(t *testing.T) {
	s := NewState()
	window := 168 * time.Hour

	// A competition-era peak outside the window must not count.
	s.PortfolioHistory = []PortfolioSample{
		{Value: 18.48, At: time.Now().Add(-10 * 24 * time.Hour).Unix()},
	}
	s.UpdatePortfolio(14.70, window)

	if peak := s.PeakOverWindow(window); peak != 14.70 {
		t.Fatalf("rolling peak = %f, want 14.70 (stale 18.48 must be ignored)", peak)
	}
	// The stale sample is also pruned from history.
	if len(s.PortfolioHistory) != 1 {
		t.Fatalf("expected stale sample pruned, history = %+v", s.PortfolioHistory)
	}
	// All-time high is still tracked for reporting.
	if s.PeakPortfolioUSD != 14.70 {
		t.Fatalf("all-time high = %f, want 14.70", s.PeakPortfolioUSD)
	}

	// A high inside the window does count.
	s.UpdatePortfolio(16.00, window)
	s.UpdatePortfolio(13.00, window)
	if peak := s.PeakOverWindow(window); peak != 16.00 {
		t.Fatalf("rolling peak = %f, want 16.00", peak)
	}
}

func TestFirstRunHasNoDrawdownLock(t *testing.T) {
	// Empty history → peak 0 → CheckPolicy must skip the drawdown stage.
	s := NewState()
	p := DefaultPolicy()
	p.DrawdownCap = 0.15

	peak := s.PeakOverWindow(p.DrawdownWindow())
	res := CheckPolicy(p, "ETH", "buy", 4, nil, nil, 14.70, peak)
	if !res.Allowed {
		t.Fatalf("buy must be allowed with no portfolio history, got %+v", res)
	}
}

func TestIndicatorSanity(t *testing.T) {
	// Monotonic rising series: RSI must be high, EMA7 above EMA30.
	rising := make([]float64, 40)
	for i := range rising {
		rising[i] = 100 + float64(i)
	}
	if rsi := RSI(rising, 14); rsi != 100 {
		t.Fatalf("RSI of strictly rising series = %f, want 100", rsi)
	}
	if EMA(rising, 7) <= EMA(rising, 30) {
		t.Fatal("EMA7 must exceed EMA30 in an uptrend")
	}

	falling := make([]float64, 40)
	for i := range falling {
		falling[i] = 200 - float64(i)
	}
	if rsi := RSI(falling, 14); rsi != 0 {
		t.Fatalf("RSI of strictly falling series = %f, want 0", rsi)
	}
}
