package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const cmcBaseURL = "https://pro-api.coinmarketcap.com"

// MarketData holds the relevant market signals for the trading strategy.
type MarketData struct {
	Symbol         string
	Price          float64
	Change24h      float64  // percentage
	Change7d       float64  // percentage
	Volume24hUSD   float64
	MarketCapUSD   float64
	FearGreedValue int      // 0-100
	FearGreedLabel string   // "Extreme Fear", "Fear", "Neutral", "Greed", "Extreme Greed"
	FetchedAt      time.Time
	// Technical indicators — populated from TWAK price history; 0 means unavailable.
	EMA7  float64 // 7-period EMA (short-term trend)
	EMA30 float64 // 30-period EMA (medium-term trend)
	RSI14 float64 // 14-period RSI, 0-100 (50 = neutral if unavailable)
}

// CMCClient calls the CoinMarketCap Pro API.
type CMCClient struct {
	APIKey  string
	BaseURL string
	http    *http.Client
}

// NewCMCClient creates a CMC API client with a 10-second timeout.
func NewCMCClient(apiKey string) *CMCClient {
	return &CMCClient{
		APIKey:  apiKey,
		BaseURL: cmcBaseURL,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// FetchMarketData retrieves price, sentiment, and trend data for a symbol.
func (c *CMCClient) FetchMarketData(symbol string) (*MarketData, error) {
	price, change24h, change7d, volume, mcap, err := c.fetchQuote(symbol)
	if err != nil {
		return nil, fmt.Errorf("fetch quote: %w", err)
	}

	fg, fgLabel, err := c.fetchFearGreed()
	if err != nil {
		// Non-fatal: proceed without F&G if CMC returns an error.
		fg = 50
		fgLabel = "Neutral"
	}

	return &MarketData{
		Symbol:         symbol,
		Price:          price,
		Change24h:      change24h,
		Change7d:       change7d,
		Volume24hUSD:   volume,
		MarketCapUSD:   mcap,
		FearGreedValue: fg,
		FearGreedLabel: fgLabel,
		FetchedAt:      time.Now(),
	}, nil
}

func (c *CMCClient) fetchQuote(symbol string) (price, change24h, change7d, volume, mcap float64, err error) {
	url := fmt.Sprintf("%s/v2/cryptocurrency/quotes/latest?symbol=%s&convert=USDT", c.BaseURL, symbol)
	body, err := c.get(url)
	if err != nil {
		return 0, 0, 0, 0, 0, err
	}

	type quoteFields struct {
		Price            float64 `json:"price"`
		PercentChange24h float64 `json:"percent_change_24h"`
		PercentChange7d  float64 `json:"percent_change_7d"`
		Volume24h        float64 `json:"volume_24h"`
		MarketCap        float64 `json:"market_cap"`
	}
	var resp struct {
		Data map[string][]struct {
			Quote map[string]quoteFields `json:"quote"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("parse quote response: %w", err)
	}

	entries, ok := resp.Data[symbol]
	if !ok || len(entries) == 0 {
		return 0, 0, 0, 0, 0, fmt.Errorf("symbol %s not found in response", symbol)
	}

	// CMC returns multiple tokens sharing the same symbol (e.g. meme tokens named "BNB").
	// Pick the entry with the highest market cap — that's always the canonical asset.
	best := entries[0]
	for _, e := range entries[1:] {
		if e.Quote["USDT"].MarketCap > best.Quote["USDT"].MarketCap {
			best = e
		}
	}

	q, ok := best.Quote["USDT"]
	if !ok {
		return 0, 0, 0, 0, 0, fmt.Errorf("USDT quote not found for %s", symbol)
	}

	return q.Price, q.PercentChange24h, q.PercentChange7d, q.Volume24h, q.MarketCap, nil
}

func (c *CMCClient) fetchFearGreed() (value int, label string, err error) {
	url := fmt.Sprintf("%s/v3/fear-and-greed/latest", c.BaseURL)
	body, err := c.get(url)
	if err != nil {
		return 0, "", err
	}

	var resp struct {
		Data struct {
			Value               int    `json:"value"`
			ValueClassification string `json:"value_classification"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, "", fmt.Errorf("parse fear-greed response: %w", err)
	}

	return resp.Data.Value, resp.Data.ValueClassification, nil
}

func (c *CMCClient) get(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-CMC_PRO_API_KEY", c.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CMC API error %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	return body, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
