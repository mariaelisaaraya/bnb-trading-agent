package main

// EMA computes the Exponential Moving Average for the given period, seeded
// with the SMA of the first `period` points (the standard construction).
// Returns 0 if there are fewer data points than the period.
func EMA(prices []float64, period int) float64 {
	if period <= 0 || len(prices) < period {
		return 0
	}
	var sum float64
	for _, p := range prices[:period] {
		sum += p
	}
	ema := sum / float64(period)
	k := 2.0 / float64(period+1)
	for _, p := range prices[period:] {
		ema = p*k + ema*(1-k)
	}
	return ema
}

// RSI computes the Wilder RSI for the given period (typically 14): the first
// `period` changes seed the averages, then Wilder smoothing runs over the
// rest of the series. Returns 50 (neutral) if there are insufficient points.
func RSI(prices []float64, period int) float64 {
	if period <= 0 || len(prices) < period+1 {
		return 50
	}

	var gains, losses float64
	for i := 1; i <= period; i++ {
		change := prices[i] - prices[i-1]
		if change > 0 {
			gains += change
		} else {
			losses -= change
		}
	}
	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	for i := period + 1; i < len(prices); i++ {
		change := prices[i] - prices[i-1]
		var gain, loss float64
		if change > 0 {
			gain = change
		} else {
			loss = -change
		}
		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
	}

	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

// CompositeScore returns a [-1, +1] buy/sell score combining EMA, RSI, F&G, and 24h trend.
// Weights: EMA=0.40, RSI=0.30, F&G=0.20, 24h=0.10.
func CompositeScore(ema7, ema30, rsi14 float64, fg int, change24h float64) float64 {
	score := 0.0
	weights := 0.0

	// EMA regime (0.40): trend direction.
	if ema7 > 0 && ema30 > 0 {
		if ema7 > ema30 {
			score += 0.40
		} else {
			score -= 0.40
		}
		weights += 0.40
	}

	// RSI contrarian (0.30): oversold = buy, overbought = sell.
	if rsi14 > 0 {
		var rsiScore float64
		switch {
		case rsi14 <= 25:
			rsiScore = 1.0
		case rsi14 <= 35:
			rsiScore = 0.6
		case rsi14 <= 45:
			rsiScore = 0.2
		case rsi14 >= 75:
			rsiScore = -1.0
		case rsi14 >= 65:
			rsiScore = -0.6
		case rsi14 >= 55:
			rsiScore = -0.2
		}
		score += rsiScore * 0.30
		weights += 0.30
	}

	// F&G mild signal (0.20): 0→-1, 50→0, 100→+1.
	fgSignal := float64(fg-50) / 50.0
	score += fgSignal * 0.20
	weights += 0.20

	// 24h trend (0.10): capped at ±10%.
	trendSignal := change24h / 10.0
	if trendSignal < -1 {
		trendSignal = -1
	}
	if trendSignal > 1 {
		trendSignal = 1
	}
	score += trendSignal * 0.10
	weights += 0.10

	if weights == 0 {
		return 0
	}
	return score / weights
}
