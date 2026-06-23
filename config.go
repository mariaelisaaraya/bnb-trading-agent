package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// AgentConfig is the full configuration for the trading agent.
type AgentConfig struct {
	CMCAPIKey            string         `yaml:"cmc_api_key"`
	TWAK                 TWAKConfig     `yaml:"twak"`
	X402                 X402Config     `yaml:"x402"`
	TradeIntervalMinutes int            `yaml:"trade_interval_minutes"`
	Strategy             StrategyConfig `yaml:"strategy"`
	Policy               TradingPolicy  `yaml:"policy"`
}

// DefaultAgentConfig returns sensible defaults for the competition.
func DefaultAgentConfig() AgentConfig {
	return AgentConfig{
		CMCAPIKey:            "",
		TWAK:                 TWAKConfig{DryRun: false},
		X402: X402Config{
			Enabled:        true,
			ServiceURL:     "https://agentsvc.io/api/search",
			PaymentAsset:   "USDT",
			MaxPaymentUSDC: 0.001,
			MinBalanceUSD:  3.0,
		},
		TradeIntervalMinutes: 30,
		Strategy:             DefaultStrategyConfig(),
		Policy:               DefaultPolicy(),
	}
}

// DefaultConfigDir returns ~/.bnb-trading-agent
func DefaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".bnb-trading-agent"
	}
	return filepath.Join(home, ".bnb-trading-agent")
}

// LoadConfig reads config from a YAML file. Returns defaults if the file
// does not exist.
func LoadConfig(path string) (AgentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultAgentConfig(), nil
		}
		return AgentConfig{}, fmt.Errorf("read config: %w", err)
	}
	cfg := DefaultAgentConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return AgentConfig{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// SaveConfig writes config to a YAML file with 0600 permissions.
func SaveConfig(path string, cfg AgentConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	header := []byte("# BNB Trading Agent configuration\n# BNB Hack 2026\n\n")
	return os.WriteFile(path, append(header, data...), 0600)
}

// EnsureConfigDir creates the config directory and writes default files
// if they don't exist.
func EnsureConfigDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		if err := SaveConfig(cfgPath, DefaultAgentConfig()); err != nil {
			return fmt.Errorf("write default config: %w", err)
		}
	}

	policyPath := filepath.Join(dir, "policy.yaml")
	if _, err := os.Stat(policyPath); os.IsNotExist(err) {
		if err := SavePolicy(policyPath, DefaultPolicy()); err != nil {
			return fmt.Errorf("write default policy: %w", err)
		}
	}

	return nil
}
