package main

import (
	"fmt"
	"regexp"
)

type CredentialMatch struct {
	Type     string
	Field    string
	Redacted string
}

type credentialPattern struct {
	re       *regexp.Regexp
	typeName string
}

var credentialPatterns = []credentialPattern{
	{regexp.MustCompile(`sk-ant-[a-zA-Z0-9_-]{10,}`), "Anthropic API key"},
	{regexp.MustCompile(`sk-[a-zA-Z0-9_-]{10,}`), "OpenAI API key"},
	{regexp.MustCompile(`ghp_[a-zA-Z0-9]{10,}`), "GitHub personal access token"},
	{regexp.MustCompile(`AKIA[0-9A-Z]{4,}`), "AWS access key"},
	{regexp.MustCompile(`xox[bpas]-[a-zA-Z0-9-]{10,}`), "Slack token"},
	{regexp.MustCompile(`glpat-[a-zA-Z0-9_-]{10,}`), "GitLab PAT"},
	{regexp.MustCompile(`-----BEGIN[A-Z ]*PRIVATE KEY-----`), "Private key PEM"},
	{regexp.MustCompile(`eyJ[a-zA-Z0-9_-]{20,}\.[a-zA-Z0-9_-]{20,}`), "JWT"},
	{regexp.MustCompile(`0x[0-9a-fA-F]{64}`), "EVM private key"},
	{regexp.MustCompile(`\b(abandon|ability|able|about|above|absent|absorb|abstract|absurd|abuse)\b.{10,200}\b(zoo|zone|zero|year|yard)\b`), "BIP-39 mnemonic"},
}

func ScanCredentials(args map[string]any) *CredentialMatch {
	return scanMap(args, "")
}

func ScanString(s, field string) *CredentialMatch {
	return scanString(s, field)
}

func scanMap(m map[string]any, prefix string) *CredentialMatch {
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if match := scanValue(v, path); match != nil {
			return match
		}
	}
	return nil
}

func scanValue(v any, path string) *CredentialMatch {
	switch val := v.(type) {
	case string:
		return scanString(val, path)
	case map[string]any:
		return scanMap(val, path)
	case []any:
		for i, item := range val {
			p := fmt.Sprintf("%s[%d]", path, i)
			if match := scanValue(item, p); match != nil {
				return match
			}
		}
	}
	return nil
}

func scanString(s, path string) *CredentialMatch {
	for _, cp := range credentialPatterns {
		loc := cp.re.FindStringIndex(s)
		if loc == nil {
			continue
		}
		matched := s[loc[0]:loc[1]]
		return &CredentialMatch{
			Type:     cp.typeName,
			Field:    path,
			Redacted: redactValue(matched),
		}
	}
	return nil
}

func redactValue(s string) string {
	if len(s) > 10 {
		return s[:10] + "***"
	}
	if len(s) > 4 {
		return s[:4] + "***"
	}
	return "***"
}
