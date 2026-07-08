package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ERC-8004 registry addresses on BNB Chain.
// Registrations are browsable at 8004scan.io (mainnet) / testnet.8004scan.io.
const (
	ERC8004IdentityRegistryBSC     = "0x8004A169FB4a3325136EB29fA0ceB6D2e539a432"
	ERC8004IdentityRegistryTestnet = "0x8004A818BFB912233c491871b3d84c89A494BD9e"
	ERC8004ReputationRegistryBSC   = "0x8004BAa17C55a88189AE136b182e5fdA19dE9b63"
)

// AgentCard is the ERC-8004 metadata document the on-chain agentURI points to.
// register(agentURI) on the IdentityRegistry mints the agent's identity NFT.
type AgentCard struct {
	Type          string              `json:"type"`
	Name          string              `json:"name"`
	Description   string              `json:"description"`
	Version       string              `json:"version"`
	WalletAddress string              `json:"walletAddress"`
	Skills        []AgentCardSkill    `json:"skills"`
	Registrations []AgentRegistration `json:"registrations"`
	TrustModels   []string            `json:"trustModels"`
}

type AgentCardSkill struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type AgentRegistration struct {
	// CAIP-10 style pointer: eip155:<chainId>:<registry address>.
	AgentRegistry string `json:"agentRegistry"`
}

// BuildAgentCard assembles the ERC-8004 agent card from the agent config.
func BuildAgentCard(cfg AgentConfig) AgentCard {
	return AgentCard{
		Type:          "https://eips.ethereum.org/EIPS/eip-8004#registration-v1",
		Name:          "bnb-trading-agent",
		Description:   "Autonomous multi-signal trading agent on BSC: EMA/RSI + Fear & Greed strategy, 5-stage security guard pipeline, tamper-evident audit trail, x402 self-funding.",
		Version:       version,
		WalletAddress: cfg.TWAK.WalletAddress,
		Skills: []AgentCardSkill{
			{ID: "market-analysis", Description: "CMC market data + EMA/RSI technical indicators"},
			{ID: "spot-trading", Description: "USDT-pair swaps on BSC via Trust Wallet Agent Kit"},
			{ID: "risk-management", Description: "Spending caps, drawdown circuit breaker, rate limits"},
			{ID: "x402-payments", Description: "Autonomous HTTP 402 micropayments for data services"},
		},
		Registrations: []AgentRegistration{
			{AgentRegistry: "eip155:56:" + ERC8004IdentityRegistryBSC},
		},
		TrustModels: []string{"reputation"},
	}
}

// WriteAgentCard writes the agent card JSON to the config dir and returns its path.
func WriteAgentCard(configDir string, cfg AgentConfig) (string, error) {
	card := BuildAgentCard(cfg)
	data, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal agent card: %w", err)
	}
	path := filepath.Join(configDir, "agent-card.json")
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return "", fmt.Errorf("write agent card: %w", err)
	}
	return path, nil
}

// PrintIdentityInstructions explains how to register the card on-chain.
func PrintIdentityInstructions(cardPath string, cfg AgentConfig) {
	fmt.Printf("\nERC-8004 agent card written to:\n  %s\n\n", cardPath)
	fmt.Println("To register this agent's on-chain identity:")
	fmt.Println("  1. Host agent-card.json at a public URL (IPFS, Greenfield, or HTTPS).")
	fmt.Println("  2. Call register(agentURI) on the IdentityRegistry with the agent wallet:")
	fmt.Printf("       BSC mainnet: %s\n", ERC8004IdentityRegistryBSC)
	fmt.Printf("       BSC testnet: %s\n", ERC8004IdentityRegistryTestnet)
	fmt.Println("  3. Verify the registration at 8004scan.io (or testnet.8004scan.io).")
	fmt.Printf("\nReputation feedback registry (mainnet): %s\n", ERC8004ReputationRegistryBSC)
	if cfg.TWAK.WalletAddress == "" {
		fmt.Println("\n[warn] twak.wallet_address is empty in config.yaml — set it and re-run so the card includes the wallet.")
	}
	fmt.Println()
}
