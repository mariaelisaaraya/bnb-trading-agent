package main

import (
	"fmt"
	"time"
)

// Signal is the strategy's output: what action to take and why.
type Signal struct {
	Action    string  // "buy", "sell", "hold"
	Token     string
	AmountUSD float64
	Price     float64
	Reason    string
	Confidence float64 // 0.0 - 1.0
}

// PositionState is the strategy-facing view of the current holding in the
// evaluated token. Zero value means "no position".
type PositionState struct {
	Qty        float64 // tokens held
	AvgEntry   float64 // average cost basis in USD (0 = unknown)
	PeakPrice  float64 // highest price observed while holding
	LastSellAt int64   // unix time of the last sell (re-buy cooldown)
}

// TokenConfig identifies a single tradeable token.
type TokenConfig struct {
	Symbol         string  `yaml:"symbol"`           // CMC symbol for market data (e.g. "CAKE")
	Contract       string  `yaml:"contract"`         // BEP-20 contract address for TWAK swaps
	TradeAmountUSD float64 `yaml:"trade_amount_usd"` // per-trade size in USD (overrides global)
}

// StrategyConfig controls the Fear & Greed + trend strategy parameters.
type StrategyConfig struct {
	// Legacy single-token fields (kept for backward compatibility).
	Token            string  `yaml:"token"`
	TokenContract    string  `yaml:"token_contract"`
	TradeAmountUSD   float64 `yaml:"trade_amount_usd"`
	// Thresholds shared across all tokens.
	FGBuyThreshold   int     `yaml:"fg_buy_threshold"`
	FGSellThreshold  int     `yaml:"fg_sell_threshold"`
	TrendBuyMinPct   float64 `yaml:"trend_buy_min_pct"`
	TrendSellMaxPct  float64 `yaml:"trend_sell_max_pct"`
	TrendBuy7dMinPct float64 `yaml:"trend_buy_7d_min_pct"`
	// Position exit rules, measured against the average entry price.
	// 0 means "use the default"; -1 disables the rule.
	StopLossPct         float64 `yaml:"stop_loss_pct"`         // sell full position if PnL <= -X% (default 8)
	TakeProfitPct       float64 `yaml:"take_profit_pct"`       // sell full position if PnL >= +X% (default 12)
	TrailingStopPct     float64 `yaml:"trailing_stop_pct"`     // give-back % from peak once armed (default 3)
	TrailingActivatePct float64 `yaml:"trailing_activate_pct"` // peak gain % that arms the trailing stop (default 5)
	CooldownMinutes     int     `yaml:"cooldown_minutes"`      // no re-buy this long after a sell (default 240)
	// Multi-token list. If set, overrides the single-token fields above.
	Tokens []TokenConfig `yaml:"tokens"`
}

// Exit-rule defaults, applied when the config value is 0 so existing
// config.yaml files gain the rules without editing. -1 disables a rule.
const (
	defaultStopLossPct         = 8.0
	defaultTakeProfitPct       = 12.0
	defaultTrailingStopPct     = 3.0
	defaultTrailingActivatePct = 5.0
	defaultCooldownMinutes     = 240
)

func exitParam(configured, def float64) float64 {
	if configured == 0 {
		return def
	}
	if configured < 0 {
		return 0 // disabled
	}
	return configured
}

func (s StrategyConfig) stopLossPct() float64     { return exitParam(s.StopLossPct, defaultStopLossPct) }
func (s StrategyConfig) takeProfitPct() float64   { return exitParam(s.TakeProfitPct, defaultTakeProfitPct) }
func (s StrategyConfig) trailingStopPct() float64 { return exitParam(s.TrailingStopPct, defaultTrailingStopPct) }
func (s StrategyConfig) trailingActivatePct() float64 {
	return exitParam(s.TrailingActivatePct, defaultTrailingActivatePct)
}
func (s StrategyConfig) cooldownMinutes() int {
	if s.CooldownMinutes == 0 {
		return defaultCooldownMinutes
	}
	if s.CooldownMinutes < 0 {
		return 0
	}
	return s.CooldownMinutes
}

// ActiveTokens returns the list of tokens to trade, supporting both the
// legacy single-token config and the new multi-token list.
func (s StrategyConfig) ActiveTokens() []TokenConfig {
	if len(s.Tokens) > 0 {
		return s.Tokens
	}
	return []TokenConfig{{
		Symbol:         s.Token,
		Contract:       s.TokenContract,
		TradeAmountUSD: s.TradeAmountUSD,
	}}
}

// DefaultStrategyConfig returns conservative defaults for the competition.
func DefaultStrategyConfig() StrategyConfig {
	return StrategyConfig{
		Token:            "BNB",
		TradeAmountUSD:   50.0,
		FGBuyThreshold:   55,
		FGSellThreshold:  30,
		TrendBuyMinPct:   1.0,
		TrendSellMaxPct:  -3.0,
		TrendBuy7dMinPct: -20.0,
	}
}

// Evaluate runs the multi-signal trading strategy against market data.
//
// SELL conditions (any is sufficient, checked in order):
//   - Position exits vs. avg entry: stop-loss, take-profit, trailing stop
//     (full-position exits, evaluated before any market signal)
//   - Fear & Greed <= FGSellThreshold (extreme fear hard exit)
//   - 24h price change <= TrendSellMaxPct (sharp drop stop-loss)
//
// BUY conditions — composite score if TA available, else F&G+trend fallback:
//   With EMA/RSI: composite score (EMA 40%, RSI 30%, F&G 20%, 24h 10%) >= 0.25
//                 OR RSI < 30 (oversold) with score > -0.15 (contrarian entry)
//   Without TA:  F&G >= FGBuyThreshold + 24h >= TrendBuyMinPct + 7d >= 7dMinPct
//   Any buy is suppressed during the post-sell cooldown window.
func Evaluate(data *MarketData, cfg StrategyConfig, pos PositionState) Signal {
	token := cfg.Token
	if data.Symbol != token {
		return Signal{
			Action: "hold",
			Token:  token,
			Price:  data.Price,
			Reason: fmt.Sprintf("market data symbol %s != configured token %s", data.Symbol, token),
		}
	}

	// Position exits come first: they manage risk on capital already deployed
	// and don't depend on market sentiment.
	if exit := evaluateExits(data.Price, cfg, pos); exit != nil {
		exit.Token = token
		return *exit
	}

	sig := evaluateMarket(data, cfg)

	// Post-sell cooldown: after exiting, wait before re-entering the same
	// token so the agent doesn't buy straight back into a falling market.
	if sig.Action == "buy" && cfg.cooldownMinutes() > 0 && pos.LastSellAt > 0 {
		elapsed := time.Since(time.Unix(pos.LastSellAt, 0))
		window := time.Duration(cfg.cooldownMinutes()) * time.Minute
		if elapsed < window {
			return Signal{
				Action: "hold",
				Token:  token,
				Price:  data.Price,
				Reason: fmt.Sprintf(
					"cooldown: sold %s ago, re-buy allowed in %s (suppressed: %s)",
					elapsed.Round(time.Minute), (window - elapsed).Round(time.Minute), sig.Reason,
				),
			}
		}
	}
	return sig
}

// evaluateExits checks stop-loss, take-profit, and trailing stop against the
// tracked position. Returns nil when no exit applies. Exits sell the full
// position (the agent caps the amount to actual holdings before executing).
func evaluateExits(price float64, cfg StrategyConfig, pos PositionState) *Signal {
	if pos.Qty <= 0 || pos.AvgEntry <= 0 || price <= 0 {
		return nil
	}
	pnlPct := (price/pos.AvgEntry - 1) * 100
	fullExitUSD := pos.Qty * price

	if sl := cfg.stopLossPct(); sl > 0 && pnlPct <= -sl {
		return &Signal{
			Action:     "sell",
			AmountUSD:  fullExitUSD,
			Price:      price,
			Confidence: 1.0,
			Reason: fmt.Sprintf(
				"stop-loss: PnL %.1f%% <= -%.1f%% (entry $%.4f)",
				pnlPct, sl, pos.AvgEntry,
			),
		}
	}

	if tp := cfg.takeProfitPct(); tp > 0 && pnlPct >= tp {
		return &Signal{
			Action:     "sell",
			AmountUSD:  fullExitUSD,
			Price:      price,
			Confidence: 1.0,
			Reason: fmt.Sprintf(
				"take-profit: PnL +%.1f%% >= +%.1f%% (entry $%.4f)",
				pnlPct, tp, pos.AvgEntry,
			),
		}
	}

	// Trailing stop: once the peak gain arms it, exit when the price gives
	// back more than trailingStopPct from the peak.
	trail, activate := cfg.trailingStopPct(), cfg.trailingActivatePct()
	if trail > 0 && pos.PeakPrice > 0 {
		peakGainPct := (pos.PeakPrice/pos.AvgEntry - 1) * 100
		giveBackPct := (1 - price/pos.PeakPrice) * 100
		if peakGainPct >= activate && giveBackPct >= trail {
			return &Signal{
				Action:     "sell",
				AmountUSD:  fullExitUSD,
				Price:      price,
				Confidence: 1.0,
				Reason: fmt.Sprintf(
					"trailing stop: %.1f%% below peak $%.4f (peak gain +%.1f%%, PnL %+.1f%%)",
					giveBackPct, pos.PeakPrice, peakGainPct, pnlPct,
				),
			}
		}
	}

	return nil
}

// evaluateMarket runs the sentiment + technical-analysis signal logic.
func evaluateMarket(data *MarketData, cfg StrategyConfig) Signal {
	token := cfg.Token

	// Hard sell: extreme fear exit.
	if data.FearGreedValue <= cfg.FGSellThreshold {
		return Signal{
			Action:     "sell",
			Token:      token,
			AmountUSD:  cfg.TradeAmountUSD,
			Price:      data.Price,
			Confidence: confidence(cfg.FGSellThreshold-data.FearGreedValue, 30),
			Reason: fmt.Sprintf(
				"extreme fear: F&G=%d (%s) <= threshold %d",
				data.FearGreedValue, data.FearGreedLabel, cfg.FGSellThreshold,
			),
		}
	}

	// Hard sell: sharp 24h drop stop-loss.
	if data.Change24h <= cfg.TrendSellMaxPct {
		return Signal{
			Action:     "sell",
			Token:      token,
			AmountUSD:  cfg.TradeAmountUSD,
			Price:      data.Price,
			Confidence: confidence(int(-data.Change24h), 10),
			Reason: fmt.Sprintf(
				"sharp drop: 24h change=%.2f%% <= threshold %.2f%%",
				data.Change24h, cfg.TrendSellMaxPct,
			),
		}
	}

	// Technical analysis path: EMA + RSI composite score.
	if data.EMA7 > 0 && data.EMA30 > 0 {
		score := CompositeScore(data.EMA7, data.EMA30, data.RSI14, data.FearGreedValue, data.Change24h)
		emaTrend := "↓ bear"
		if data.EMA7 > data.EMA30 {
			emaTrend = "↑ bull"
		}

		// Trend-following buy: EMA uptrend + strong composite score.
		if data.EMA7 > data.EMA30 && score >= 0.25 {
			return Signal{
				Action:     "buy",
				Token:      token,
				AmountUSD:  cfg.TradeAmountUSD,
				Price:      data.Price,
				Confidence: score,
				Reason: fmt.Sprintf(
					"TA buy: score=%.2f, EMA %s, RSI=%.1f, F&G=%d",
					score, emaTrend, data.RSI14, data.FearGreedValue,
				),
			}
		}

		// Contrarian buy: RSI oversold — buy the dip even in mild downtrend.
		// Threshold -0.30 captures extreme oversold (RSI < 30) even with EMA bearish.
		if data.RSI14 > 0 && data.RSI14 < 30 && score > -0.30 {
			return Signal{
				Action:     "buy",
				Token:      token,
				AmountUSD:  cfg.TradeAmountUSD,
				Price:      data.Price,
				Confidence: (30 - data.RSI14) / 30,
				Reason: fmt.Sprintf(
					"oversold buy: RSI=%.1f < 30, score=%.2f, EMA %s",
					data.RSI14, score, emaTrend,
				),
			}
		}

		return Signal{
			Action: "hold",
			Token:  token,
			Price:  data.Price,
			Reason: fmt.Sprintf(
				"hold: score=%.2f (need ≥0.25), EMA %s, RSI=%.1f, F&G=%d",
				score, emaTrend, data.RSI14, data.FearGreedValue,
			),
		}
	}

	// Fallback: original F&G + trend logic (no TA available).
	fgBuyOk := data.FearGreedValue >= cfg.FGBuyThreshold
	trendOk := data.Change24h >= cfg.TrendBuyMinPct
	weekOk := data.Change7d >= cfg.TrendBuy7dMinPct

	if fgBuyOk && trendOk && weekOk {
		conf := (confidence(data.FearGreedValue-cfg.FGBuyThreshold, 45) +
			confidence(int(data.Change24h-cfg.TrendBuyMinPct), 10)) / 2
		return Signal{
			Action:     "buy",
			Token:      token,
			AmountUSD:  cfg.TradeAmountUSD,
			Price:      data.Price,
			Confidence: conf,
			Reason: fmt.Sprintf(
				"momentum: F&G=%d (%s), 24h=+%.2f%%, 7d=+%.2f%%",
				data.FearGreedValue, data.FearGreedLabel, data.Change24h, data.Change7d,
			),
		}
	}

	return Signal{
		Action: "hold",
		Token:  token,
		Price:  data.Price,
		Reason: fmt.Sprintf(
			"hold: F&G=%d, 24h=%.2f%%, 7d=%.2f%% — buy needs F&G>=%d + 24h>=%.1f%% + 7d>=%.1f%%",
			data.FearGreedValue, data.Change24h, data.Change7d,
			cfg.FGBuyThreshold, cfg.TrendBuyMinPct, cfg.TrendBuy7dMinPct,
		),
	}
}

// confidence converts a raw delta into a 0.0–1.0 confidence score.
func confidence(delta, max int) float64 {
	if delta <= 0 {
		return 0.0
	}
	if delta >= max {
		return 1.0
	}
	return float64(delta) / float64(max)
}
