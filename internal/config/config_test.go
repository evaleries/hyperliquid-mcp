package config

import (
	"testing"
)

func envFrom(pairs map[string]string) func(string) string {
	return func(k string) string { return pairs[k] }
}

func TestLoadRequiresPrivateKey(t *testing.T) {
	if _, err := LoadFromEnv(envFrom(nil)); err == nil {
		t.Fatal("missing key must error")
	}
}

func TestLoadParsesKeyAndDerivesAccount(t *testing.T) {
	cfg, err := LoadFromEnv(envFrom(map[string]string{
		EnvPrivateKey: "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.WalletAddress != "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266" {
		t.Errorf("wallet: %s", cfg.WalletAddress)
	}
	if cfg.AccountAddress != cfg.WalletAddress {
		t.Errorf("account should default to wallet: %s", cfg.AccountAddress)
	}
	if cfg.Testnet {
		t.Error("default must be mainnet")
	}
	if cfg.BaseURL != "https://api.hyperliquid.xyz" {
		t.Errorf("baseURL: %s", cfg.BaseURL)
	}
}

func TestLoadAgentModeAndTestnet(t *testing.T) {
	cfg, err := LoadFromEnv(envFrom(map[string]string{
		EnvPrivateKey:     "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80", // no 0x prefix
		EnvAccountAddress: "0x000000000000000000000000000000000000dead",
		EnvTestnet:        "TrUe",
		EnvVaultAddress:   "0x000000000000000000000000000000000000beef",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.AccountAddress != "0x000000000000000000000000000000000000dead" {
		t.Errorf("agent account: %s", cfg.AccountAddress)
	}
	if !cfg.Testnet || cfg.BaseURL != "https://api.hyperliquid-testnet.xyz" {
		t.Errorf("testnet: %v %s", cfg.Testnet, cfg.BaseURL)
	}
	if cfg.VaultAddress != "0x000000000000000000000000000000000000beef" {
		t.Errorf("vault: %s", cfg.VaultAddress)
	}
}

func TestLoadBaseURLOverride(t *testing.T) {
	const pk = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	tests := []struct {
		name    string
		baseURL string
		testnet string
		want    string
		wantErr bool
	}{
		{"unset keeps mainnet default", "", "", "https://api.hyperliquid.xyz", false},
		{"unset keeps testnet default", "", "true", "https://api.hyperliquid-testnet.xyz", false},
		{"override on mainnet", "https://hl-proxy.example.com", "", "https://hl-proxy.example.com", false},
		{"override beats testnet flag", "https://localhost:8080", "true", "https://localhost:8080", false},
		{"http allowed for local mocks", "http://127.0.0.1:9000", "", "http://127.0.0.1:9000", false},
		{"trailing slash trimmed", "https://api.hyperliquid.xyz/", "", "https://api.hyperliquid.xyz", false},
		{"whitespace tolerated", "  https://api.hyperliquid.xyz  ", "", "https://api.hyperliquid.xyz", false},
		{"path kept for proxied mounts", "https://proxy.example.com/hl/", "", "https://proxy.example.com/hl", false},
		{"whitespace-only is unset", "   ", "", "https://api.hyperliquid.xyz", false},
		{"missing scheme", "api.hyperliquid.xyz", "", "", true},
		{"non-http scheme", "ftp://api.hyperliquid.xyz", "", "", true},
		{"missing host", "https://", "", "", true},
		{"query rejected", "https://api.hyperliquid.xyz?x=1", "", "", true},
		{"fragment rejected", "https://api.hyperliquid.xyz#frag", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{EnvPrivateKey: pk}
			if tt.baseURL != "" {
				env[EnvBaseURL] = tt.baseURL
			}
			if tt.testnet != "" {
				env[EnvTestnet] = tt.testnet
			}
			cfg, err := LoadFromEnv(envFrom(env))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got baseURL %q", tt.baseURL, cfg.BaseURL)
				}
				return
			}
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if cfg.BaseURL != tt.want {
				t.Errorf("baseURL = %q, want %q", cfg.BaseURL, tt.want)
			}
		})
	}
}

func TestLoadRejectsBadKey(t *testing.T) {
	if _, err := LoadFromEnv(envFrom(map[string]string{EnvPrivateKey: "not-hex"})); err == nil {
		t.Fatal("bad key must error")
	}
}

func TestTestnetFlagStrictness(t *testing.T) {
	// Exact Python parity: os.getenv(...).lower() == "true" — no trimming.
	tests := []struct {
		value   string
		testnet bool
	}{
		{"true", true},
		{"TrUe", true},
		{"TRUE", true},
		{"1", false},
		{"yes", false},
		{"", false},
		{" true", false}, // untrimmed: Python selects mainnet here too
		{"TRUE ", false},
	}
	for _, tt := range tests {
		cfg, err := LoadFromEnv(envFrom(map[string]string{
			EnvPrivateKey: "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
			EnvTestnet:    tt.value,
		}))
		if err != nil {
			t.Fatalf("load %q: %v", tt.value, err)
		}
		if cfg.Testnet != tt.testnet {
			t.Errorf("HYPERLIQUID_TESTNET=%q: testnet=%v, want %v", tt.value, cfg.Testnet, tt.testnet)
		}
	}
}
