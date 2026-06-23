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

// StrategyConfig controls the Fear & Greed + trend strategy parameters.
type StrategyConfig struct {
	Token            string  `yaml:"token"`
	TradeAmountUSD   float64 `yaml:"trade_amount_usd"`
	FGBuyThreshold   int     `yaml:"fg_buy_threshold"`    // buy when F&G >= this
	FGSellThreshold  int     `yaml:"fg_sell_threshold"`   // sell when F&G <= this
	TrendBuyMinPct   float64 `yaml:"trend_buy_min_pct"`   // 24h change must be >= this to buy
	TrendSellMaxPct  float64 `yaml:"trend_sell_max_pct"`  // 24h change <= this triggers sell
	TrendBuy7dMinPct float64 `yaml:"trend_buy_7d_min_pct"` // 7d change floor for buy (-20 = disabled)
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

// Evaluate runs the Fear & Greed + momentum strategy against market data.
//
// BUY conditions (all must be true):
//   - Fear & Greed >= FGBuyThreshold (market is greedy = momentum)
//   - 24h price change >= TrendBuyMinPct (positive short-term trend)
//   - 7d price change >= 0 (positive medium-term trend)
//
// SELL conditions (any is sufficient):
//   - Fear & Greed <= FGSellThreshold (extreme fear = exit)
//   - 24h price change <= TrendSellMaxPct (sharp drop = stop loss)
//
// HOLD otherwise.
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

	// Sell signals take priority.
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

	// Buy signals: all conditions must be true.
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

	// Build a human-readable hold reason.
	holdReason := fmt.Sprintf(
		"hold: F&G=%d, 24h=%.2f%%, 7d=%.2f%% — buy needs F&G>=%d + 24h>=%.1f%% + 7d>=0",
		data.FearGreedValue, data.Change24h, data.Change7d,
		cfg.FGBuyThreshold, cfg.TrendBuyMinPct,
	)

	return Signal{
		Action: "hold",
		Token:  token,
		Price:  data.Price,
		Reason: holdReason,
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
