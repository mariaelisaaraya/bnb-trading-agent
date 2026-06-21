package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type AuditEntry struct {
	Timestamp string   `json:"timestamp"`
	TradeID   string   `json:"trade_id"`
	Action    string   `json:"action"`
	Token     string   `json:"token,omitempty"`
	Direction string   `json:"direction,omitempty"`
	AmountUSD float64  `json:"amount_usd,omitempty"`
	Price     float64  `json:"price,omitempty"`
	Decision  string   `json:"decision"`
	Reason    string   `json:"reason,omitempty"`
	Stages    []string `json:"pipeline_stages"`
	PrevHash  string   `json:"prev_hash"`
	Hash      string   `json:"hash"`
}

type AuditLog struct {
	path     string
	prevHash string
}

const genesisHash = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

func NewAuditLog(path string) (*AuditLog, error) {
	al := &AuditLog{path: path, prevHash: genesisHash}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return al, nil
		}
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()

	var lastLine string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lastLine = scanner.Text()
	}
	if lastLine != "" {
		var entry AuditEntry
		if err := json.Unmarshal([]byte(lastLine), &entry); err == nil && entry.Hash != "" {
			al.prevHash = entry.Hash
		}
	}
	return al, nil
}

func (al *AuditLog) Log(entry AuditEntry) error {
	entry.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	entry.PrevHash = al.prevHash
	entry.Hash = computeEntryHash(entry)
	al.prevHash = entry.Hash

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal audit entry: %w", err)
	}

	f, err := os.OpenFile(al.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open audit log for write: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write audit entry: %w", err)
	}
	return nil
}

func ReadEntries(path string) ([]AuditEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()

	var entries []AuditEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var e AuditEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, scanner.Err()
}

func VerifyChain(entries []AuditEntry) int {
	prev := genesisHash
	for i, e := range entries {
		if e.PrevHash != prev {
			return i
		}
		if computeEntryHash(e) != e.Hash {
			return i
		}
		prev = e.Hash
	}
	return -1
}

func computeEntryHash(e AuditEntry) string {
	payload := struct {
		Timestamp string   `json:"timestamp"`
		TradeID   string   `json:"trade_id"`
		Action    string   `json:"action"`
		Token     string   `json:"token,omitempty"`
		Direction string   `json:"direction,omitempty"`
		AmountUSD float64  `json:"amount_usd,omitempty"`
		Price     float64  `json:"price,omitempty"`
		Decision  string   `json:"decision"`
		Reason    string   `json:"reason,omitempty"`
		Stages    []string `json:"pipeline_stages"`
		PrevHash  string   `json:"prev_hash"`
	}{
		Timestamp: e.Timestamp,
		TradeID:   e.TradeID,
		Action:    e.Action,
		Token:     e.Token,
		Direction: e.Direction,
		AmountUSD: e.AmountUSD,
		Price:     e.Price,
		Decision:  e.Decision,
		Reason:    e.Reason,
		Stages:    e.Stages,
		PrevHash:  e.PrevHash,
	}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum[:])
}
