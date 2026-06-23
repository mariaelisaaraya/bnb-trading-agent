package main

import (
	"encoding/json"
	"fmt"
	"os"
	"syscall"
	"time"
)

// State persists trade counters, spend records, trade intents, and portfolio
// tracking between agent loop iterations. Serialized to disk with file locking.
type State struct {
	Calls               map[string][]int64       `json:"calls"`
	Spends              map[string][]SpendRecord `json:"spends"`
	Intents             map[string]TradeIntent   `json:"intents"`
	Holdings            map[string]float64       `json:"holdings"` // token symbol → quantity held
	PeakPortfolioUSD    float64                  `json:"peak_portfolio_usd"`
	CurrentPortfolioUSD float64                  `json:"current_portfolio_usd"`
	TotalTradesExecuted int                      `json:"total_trades_executed"`
}

const tradeStateKey = "__bnbagent_trade__"

// NewState creates an empty state.
func NewState() *State {
	return &State{
		Calls:    make(map[string][]int64),
		Spends:   make(map[string][]SpendRecord),
		Intents:  make(map[string]TradeIntent),
		Holdings: make(map[string]float64),
	}
}

// LoadState reads state from disk. Returns empty state if the file does not exist.
// Prunes entries older than 24h on load.
func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewState(), nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	s := NewState()
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	s.prune()
	return s, nil
}

// Save writes state to disk atomically with 0600 permissions.
func (s *State) Save(path string) error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write state tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename state: %w", err)
	}
	return nil
}

// RecordTradeCall records a trade execution timestamp for rate limiting.
func (s *State) RecordTradeCall() {
	s.Calls[tradeStateKey] = append(s.Calls[tradeStateKey], time.Now().Unix())
	s.TotalTradesExecuted++
}

// RecordTradeSpend records a trade spend amount for daily cap tracking.
func (s *State) RecordTradeSpend(amountUSD float64) {
	if amountUSD <= 0 {
		return
	}
	s.Spends[tradeStateKey] = append(s.Spends[tradeStateKey], SpendRecord{
		Amount: amountUSD,
		At:     time.Now().Unix(),
	})
}

// RegisterIntent stores a trade intent if one does not already exist for the key.
func (s *State) RegisterIntent(key, token, direction string, amountUSD, price float64) bool {
	if _, exists := s.Intents[key]; exists {
		return false
	}
	s.Intents[key] = TradeIntent{
		Token:         token,
		Direction:     direction,
		AmountUSD:     amountUSD,
		ExpectedPrice: price,
		RegisteredAt:  time.Now().Unix(),
		Hash:          HashIntent(token, direction, amountUSD, price),
	}
	return true
}

// GetIntent retrieves a registered trade intent by key, or nil if none exists.
func (s *State) GetIntent(key string) *TradeIntent {
	intent, ok := s.Intents[key]
	if !ok {
		return nil
	}
	return &intent
}

// RecordBuy increases the tracked holding for a token.
func (s *State) RecordBuy(token string, amountUSD, price float64) {
	if price <= 0 || amountUSD <= 0 {
		return
	}
	if s.Holdings == nil {
		s.Holdings = make(map[string]float64)
	}
	s.Holdings[token] += amountUSD / price
}

// RecordSell decreases the tracked holding for a token (floor at zero).
func (s *State) RecordSell(token string, amountUSD, price float64) {
	if price <= 0 || amountUSD <= 0 || s.Holdings == nil {
		return
	}
	s.Holdings[token] -= amountUSD / price
	if s.Holdings[token] < 0 {
		s.Holdings[token] = 0
	}
}

// UpdatePortfolio updates the portfolio value and tracks the peak.
func (s *State) UpdatePortfolio(currentValueUSD float64) {
	s.CurrentPortfolioUSD = currentValueUSD
	if currentValueUSD > s.PeakPortfolioUSD {
		s.PeakPortfolioUSD = currentValueUSD
	}
}

// GetCalls returns recent call timestamps for rate limiting.
func (s *State) GetCalls() []int64 {
	return s.Calls[tradeStateKey]
}

// GetSpends returns recent spend records for daily cap tracking.
func (s *State) GetSpends() []SpendRecord {
	return s.Spends[tradeStateKey]
}

func (s *State) prune() {
	cutoff := time.Now().Add(-25 * time.Hour).Unix()

	for tool, timestamps := range s.Calls {
		pruned := timestamps[:0]
		for _, ts := range timestamps {
			if ts > cutoff {
				pruned = append(pruned, ts)
			}
		}
		if len(pruned) == 0 {
			delete(s.Calls, tool)
		} else {
			s.Calls[tool] = pruned
		}
	}

	for tool, records := range s.Spends {
		pruned := records[:0]
		for _, r := range records {
			if r.At > cutoff {
				pruned = append(pruned, r)
			}
		}
		if len(pruned) == 0 {
			delete(s.Spends, tool)
		} else {
			s.Spends[tool] = pruned
		}
	}

	intentCutoff := time.Now().Add(-7 * 24 * time.Hour).Unix()
	for key, intent := range s.Intents {
		if intent.RegisteredAt < intentCutoff {
			delete(s.Intents, key)
		}
	}
}

// FileLock holds an OS-level advisory lock on a file descriptor.
type FileLock struct {
	f *os.File
}

// AcquireLock obtains an exclusive lock. Call Release() when done.
func AcquireLock(path string) (*FileLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	return &FileLock{f: f}, nil
}

// Release releases the file lock and removes the lock file.
func (fl *FileLock) Release() {
	if fl.f == nil {
		return
	}
	name := fl.f.Name()
	_ = syscall.Flock(int(fl.f.Fd()), syscall.LOCK_UN)
	fl.f.Close()
	fl.f = nil
	os.Remove(name)
}
