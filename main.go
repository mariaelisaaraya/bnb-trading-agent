package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var version = "0.1.0"

func main() {
	root := &cobra.Command{
		Use:          "bnb-agent",
		Short:        "Autonomous BNB Chain trading agent with 5-stage security pipeline",
		SilenceUsage: true,
	}

	root.AddCommand(
		newRunCmd(),
		newRegisterCmd(),
		newAuditCmd(),
		newDemoCmd(),
		newInitCmd(),
		newVersionCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRunCmd() *cobra.Command {
	var (
		configDir string
		dryRun    bool
		once      bool
		verbose   bool
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Start the autonomous trading loop",
		Long: `Run the BNB trading agent. Fetches CMC market data, evaluates the
Fear & Greed + momentum strategy, runs every trade through the 5-stage
security pipeline, and executes via Trust Wallet Agent Kit.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if configDir == "" {
				configDir = DefaultConfigDir()
			}
			if err := EnsureConfigDir(configDir); err != nil {
				return err
			}

			cfg, err := LoadConfig(filepath.Join(configDir, "config.yaml"))
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			if dryRun {
				cfg.TWAK.DryRun = true
			}

			agent, err := NewAgent(cfg, configDir)
			if err != nil {
				return err
			}

			if once {
				return agent.RunOnce(verbose)
			}
			return agent.Run(verbose)
		},
	}
	cmd.Flags().StringVar(&configDir, "config-dir", "", "config directory (default: ~/.bnb-trading-agent)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "log trades without executing on-chain")
	cmd.Flags().BoolVar(&once, "once", false, "run one iteration and exit")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "print detailed market data")
	return cmd
}

func newRegisterCmd() *cobra.Command {
	var configDir string
	return &cobra.Command{
		Use:   "register",
		Short: "Register agent wallet in the BSC competition contract",
		Long:  "Calls `twak compete register` to submit the agent's wallet address to the on-chain participant list.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configDir == "" {
				configDir = DefaultConfigDir()
			}
			cfg, err := LoadConfig(filepath.Join(configDir, "config.yaml"))
			if err != nil {
				return err
			}
			client := NewTWAKClient(cfg.TWAK)
			return client.Register()
		},
	}
}

func newAuditCmd() *cobra.Command {
	var (
		configDir string
		verify    bool
	)
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Show tamper-evident trade audit trail",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configDir == "" {
				configDir = DefaultConfigDir()
			}
			return PrintAudit(configDir, verify)
		},
	}
	cmd.Flags().StringVar(&configDir, "config-dir", "", "config directory")
	cmd.Flags().BoolVar(&verify, "verify", false, "verify SHA-256 hash chain integrity")
	return cmd
}

func newDemoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "demo",
		Short: "Run guard pipeline demo with attack scenarios",
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunDemo()
		},
	}
}

func newInitCmd() *cobra.Command {
	var configDir string
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize config directory with default files",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configDir == "" {
				configDir = DefaultConfigDir()
			}
			if err := EnsureConfigDir(configDir); err != nil {
				return err
			}
			fmt.Printf("\nBNB Trading Agent initialized.\n\n")
			fmt.Printf("  Config:  %s/config.yaml\n", configDir)
			fmt.Printf("  Policy:  %s/policy.yaml\n", configDir)
			fmt.Printf("  Audit:   %s/audit.jsonl\n\n", configDir)
			fmt.Println("Next steps:")
			fmt.Printf("  1. Edit %s/config.yaml — set cmc_api_key and twak.wallet_address\n", configDir)
			fmt.Println("  2. bnb-agent register  — register on BSC competition contract")
			fmt.Println("  3. bnb-agent run --dry-run  — test without real trades")
			fmt.Println("  4. bnb-agent run  — go live\n")
			return nil
		},
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("bnb-agent %s\n", version)
		},
	}
}

// RunDemo simulates the guard pipeline with realistic attack scenarios.
func RunDemo() error {
	fmt.Printf("\nBNB Trading Agent — Guard Pipeline Demo\n")
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println()

	tmpDir, err := os.MkdirTemp("", "bnb-agent-demo-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	if err := EnsureConfigDir(tmpDir); err != nil {
		return err
	}

	scenarios := []struct {
		name     string
		decision TradeDecision
		wantBlock bool
	}{
		{
			name: "Normal buy — F&G=65, momentum positive",
			decision: TradeDecision{
				Token: "BNB", Direction: "buy",
				AmountUSD: 50.0, Price: 650.0,
				Reason: "F&G=65 (Greed), 24h=+2.1%, 7d=+5.3%",
			},
			wantBlock: false,
		},
		{
			name: "Same buy repeated — integrity verified",
			decision: TradeDecision{
				Token: "BNB", Direction: "buy",
				AmountUSD: 50.0, Price: 651.0,
				Reason: "F&G=67 (Greed), 24h=+2.3%",
			},
			wantBlock: false,
		},
		{
			name: "Amount inflation attack — $50 inflated to $5000",
			decision: TradeDecision{
				Token: "BNB", Direction: "buy",
				AmountUSD: 5000.0, Price: 650.0,
				Reason: "F&G=65 (Greed)",
			},
			wantBlock: true,
		},
		{
			name: "Token swap attack — BNB swapped for unknown token",
			decision: TradeDecision{
				Token: "RUGPULL", Direction: "buy",
				AmountUSD: 50.0, Price: 0.001,
				Reason: "F&G=65",
			},
			wantBlock: true,
		},
		{
			name: "Price manipulation — slippage 40% beyond expected",
			decision: TradeDecision{
				Token: "BNB", Direction: "sell",
				AmountUSD: 50.0, Price: 390.0, // 40% below $650
				Reason: "F&G=20 (Extreme Fear)",
			},
			wantBlock: false, // first sell intent — registers baseline
		},
		{
			name: "Credential exfiltration — private key in reason field",
			decision: TradeDecision{
				Token: "BNB", Direction: "buy",
				AmountUSD: 50.0, Price: 650.0,
				Reason: "buy signal 0x" + strings.Repeat("a", 64),
			},
			wantBlock: true,
		},
		{
			name: "Rate limit flood — 5th trade in < 1 hour",
			decision: TradeDecision{
				Token: "CAKE", Direction: "buy",
				AmountUSD: 50.0, Price: 2.5,
				Reason: "momentum signal",
			},
			wantBlock: false,
		},
	}

	for i, s := range scenarios {
		guard, err := NewTradeGuard(tmpDir)
		if err != nil {
			return fmt.Errorf("init guard: %w", err)
		}

		tradeID := fmt.Sprintf("demo_%d_%d", i+1, time.Now().UnixNano())
		result := guard.Run(tradeID, s.decision)
		guard.Close()

		status := "✓ ALLOW"
		if result.Decision == "block" {
			status = "✗ BLOCK"
		}

		fmt.Printf("Scenario %d: %s\n", i+1, s.name)
		fmt.Printf("  Decision:  %s\n", status)
		fmt.Printf("  Pipeline:  %s\n", joinStages(result))
		if result.Reason != "" {
			fmt.Printf("  Reason:    %s\n", result.Reason)
		}
		fmt.Println()
	}

	return nil
}
