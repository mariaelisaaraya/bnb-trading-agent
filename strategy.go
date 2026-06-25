package main

import "fmt"

// Signal is the strategy's output: what action to take and why.
type Signal struct {
	Action    string  // "buy", "sell", "hold"
	Token     string
	AmountUSD float64
	Price     float64
	Reason    string
	Confidence float64 // 0.0 - 1.0
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
	// Multi-token list. If set, overrides the single-token fields above.
	Tokens []TokenConfig `yaml:"tokens"`
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
// SELL conditions (any is sufficient, checked first):
//   - Fear & Greed <= FGSellThreshold (extreme fear hard exit)
//   - 24h price change <= TrendSellMaxPct (sharp drop stop-loss)
//
// BUY conditions — composite score if TA available, else F&G+trend fallback:
//   With EMA/RSI: composite score (EMA 40%, RSI 30%, F&G 20%, 24h 10%) >= 0.25
//                 OR RSI < 30 (oversold) with score > -0.15 (contrarian entry)
//   Without TA:  F&G >= FGBuyThreshold + 24h >= TrendBuyMinPct + 7d >= 7dMinPct
func Evaluate(data *MarketData, cfg StrategyConfig) Signal {
	token := cfg.Token
	if data.Symbol != token {
		return Signal{
			Action: "hold",
			Token:  token,
			Price:  data.Price,
			Reason: fmt.Sprintf("market data symbol %s != configured token %s", data.Symbol, token),
		}
	}

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
