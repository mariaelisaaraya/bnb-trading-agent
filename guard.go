package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

type TradeDecision struct {
	Token     string
	Direction string
	AmountUSD float64
	Price     float64
	Reason    string
}

type StageResult struct {
	Name   string
	Status string
}

type GuardResult struct {
	Decision string
	Reason   string
	Stages   []StageResult
}

func (r GuardResult) StageStrings() []string {
	out := make([]string, len(r.Stages))
	for i, s := range r.Stages {
		out[i] = s.Name + ":" + s.Status
	}
	return out
}

type TradeGuard struct {
	policy    TradingPolicy
	state     *State
	auditLog  *AuditLog
	statePath string
	lock      *FileLock
}

func NewTradeGuard(configDir string) (*TradeGuard, error) {
	policyPath := filepath.Join(configDir, "policy.yaml")
	policy, err := LoadPolicy(policyPath)
	if err != nil {
		return nil, fmt.Errorf("load policy: %w", err)
	}

	statePath := filepath.Join(configDir, "state.json")
	lockPath := filepath.Join(configDir, "state.lock")

	lock, err := AcquireLock(lockPath)
	if err != nil {
		return nil, fmt.Errorf("acquire state lock: %w", err)
	}

	state, err := LoadState(statePath)
	if err != nil {
		lock.Release()
		return nil, fmt.Errorf("load state: %w", err)
	}

	auditPath := filepath.Join(configDir, "audit.jsonl")
	auditLog, err := NewAuditLog(auditPath)
	if err != nil {
		lock.Release()
		return nil, fmt.Errorf("open audit log: %w", err)
	}

	return &TradeGuard{
		policy:    policy,
		state:     state,
		auditLog:  auditLog,
		statePath: statePath,
		lock:      lock,
	}, nil
}

func (g *TradeGuard) Run(tradeID string, d TradeDecision) GuardResult {
	result := GuardResult{}

	fields := map[string]any{
		"token":     d.Token,
		"direction": d.Direction,
		"reason":    d.Reason,
	}
	if cred := ScanCredentials(fields); cred != nil {
		result.Decision = "block"
		result.Reason = fmt.Sprintf("credential detected in trade field %q: %s (%s)", cred.Field, cred.Type, cred.Redacted)
		result.Stages = []StageResult{{Name: "credentials", Status: "DETECTED"}}
		g.logAudit(tradeID, d, result)
		return result
	}
	result.Stages = append(result.Stages, StageResult{Name: "credentials", Status: "clean"})

	policyResult := CheckPolicy(
		g.policy,
		d.Token,
		d.AmountUSD,
		g.state.GetCalls(),
		g.state.GetSpends(),
		g.state.CurrentPortfolioUSD,
		g.state.PeakPortfolioUSD,
	)
	if !policyResult.Allowed {
		result.Decision = "block"
		result.Reason = policyResult.Reason
		result.Stages = append(result.Stages, StageResult{
			Name:   "policy",
			Status: string(policyResult.Decision),
		})
		g.logAudit(tradeID, d, result)
		return result
	}
	result.Stages = append(result.Stages, StageResult{Name: "policy", Status: "allow"})

	// Check token and direction only — price changes naturally over a multi-day window.
	// Point-in-time slippage is enforced by TWAK's --slippage flag at execution.
	key := IntentKey(d.Token, d.Direction)
	intent := g.state.GetIntent(key)

	if intent == nil {
		g.state.RegisterIntent(key, d.Token, d.Direction, d.AmountUSD, d.Price)
		result.Stages = append(result.Stages, StageResult{Name: "integrity", Status: "registered"})
	} else {
		drift := CheckTradeDrift(*intent, d.Token, d.Direction, d.AmountUSD, d.Price, 1.0)
		if drift.HasDrift && (drift.Type == DriftToken || drift.Type == DriftDirection) {
			result.Decision = "block"
			result.Reason = fmt.Sprintf("%s tampered: expected %s, got %s", drift.Type, drift.Expected, drift.Got)
			result.Stages = append(result.Stages, StageResult{
				Name:   "integrity",
				Status: fmt.Sprintf("%s_TAMPERED", strings.ToUpper(string(drift.Type))),
			})
			g.logAudit(tradeID, d, result)
			return result
		}
		result.Stages = append(result.Stages, StageResult{Name: "integrity", Status: "verified"})
	}

	g.state.RecordTradeCall()
	g.state.RecordTradeSpend(d.AmountUSD)

	result.Decision = "allow"
	g.logAudit(tradeID, d, result)
	return result
}

func (g *TradeGuard) Close() {
	if g.state != nil && g.statePath != "" {
		_ = g.state.Save(g.statePath)
	}
	if g.lock != nil {
		g.lock.Release()
	}
}

func (g *TradeGuard) UpdatePortfolio(currentUSD float64) {
	g.state.UpdatePortfolio(currentUSD)
}

// RecordHolding updates the tracked position for a token after a trade executes.
func (g *TradeGuard) RecordHolding(token, direction string, amountUSD, price float64) {
	switch direction {
	case "buy":
		g.state.RecordBuy(token, amountUSD, price)
	case "sell":
		g.state.RecordSell(token, amountUSD, price)
	}
}

// EstimateHoldingsUSD returns the USD value of tracked token holdings at given prices.
func (g *TradeGuard) EstimateHoldingsUSD(prices map[string]float64) float64 {
	var total float64
	for token, qty := range g.state.Holdings {
		if price, ok := prices[token]; ok && qty > 0 {
			total += qty * price
		}
	}
	return total
}

func (g *TradeGuard) logAudit(tradeID string, d TradeDecision, result GuardResult) {
	entry := AuditEntry{
		TradeID:   tradeID,
		Action:    actionLabel(d.Direction, result.Decision),
		Token:     d.Token,
		Direction: d.Direction,
		AmountUSD: d.AmountUSD,
		Price:     d.Price,
		Decision:  result.Decision,
		Reason:    result.Reason,
		Stages:    result.StageStrings(),
	}
	_ = g.auditLog.Log(entry)
}

func actionLabel(direction, decision string) string {
	if decision == "block" {
		return "blocked_" + direction
	}
	return direction
}
