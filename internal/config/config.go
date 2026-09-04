// Package config loads and validates the server configuration from environment
// variables. Variable names and semantics are identical to the Python reference
// implementation.
package config

import (
	"crypto/ecdsa"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sonirico/go-hyperliquid"
)

// Environment variable names (contract: Python server.py).
const (
	EnvPrivateKey     = "HYPERLIQUID_PRIVATE_KEY"
	EnvAccountAddress = "HYPERLIQUID_ACCOUNT_ADDRESS"
	EnvTestnet        = "HYPERLIQUID_TESTNET"
	EnvVaultAddress   = "HYPERLIQUID_VAULT_ADDRESS"
	// EnvBaseURL is a Go extension with no Python counterpart: it
	// overrides the API base URL implied by EnvTestnet.
	EnvBaseURL = "HYPERLIQUID_BASE_URL"
)

// Config is the resolved server configuration.
type Config struct {
	// PrivateKey is the signing key parsed from HYPERLIQUID_PRIVATE_KEY.
	PrivateKey *ecdsa.PrivateKey
	// WalletAddress is derived from PrivateKey; it signs the actions.
	WalletAddress string
	// AccountAddress is the trading account (equals WalletAddress unless
	// HYPERLIQUID_ACCOUNT_ADDRESS sets agent/API-wallet mode).
	AccountAddress string
	// VaultAddress is optional vault routing (HYPERLIQUID_VAULT_ADDRESS).
	VaultAddress string
	// Testnet selects the testnet API when HYPERLIQUID_TESTNET == "true"
	// (case-insensitive); anything else means mainnet. It only selects the
	// default BaseURL; HYPERLIQUID_BASE_URL wins when set.
	Testnet bool
	// BaseURL is the Hyperliquid API base URL: HYPERLIQUID_BASE_URL when set,
	// otherwise the mainnet/testnet default implied by Testnet.
	BaseURL string
}

// Load reads configuration from the process environment.
func Load() (Config, error) {
	return LoadFromEnv(os.Getenv)
}

// LoadFromEnv reads configuration using the given lookup function
// (dependency-injected for tests).
func LoadFromEnv(getenv func(string) string) (Config, error) {
	raw := getenv(EnvPrivateKey)
	if raw == "" {
		return Config{}, fmt.Errorf("%s environment variable is required", EnvPrivateKey)
	}

	// Surrounding whitespace on the key is tolerated (ergonomic superset;
	// Python's Account.from_key would reject it). The parse error itself is
	// generic so no byte of key material reaches logs (security review
	// SEC-SECRET-004).
	key, err := crypto.HexToECDSA(strings.TrimPrefix(strings.TrimSpace(raw), "0x"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid %s: not a valid 32-byte hex secp256k1 key", EnvPrivateKey)
	}

	cfg := Config{
		PrivateKey:    key,
		VaultAddress:  getenv(EnvVaultAddress),
		Testnet:       strings.EqualFold(getenv(EnvTestnet), "true"),
		WalletAddress: crypto.PubkeyToAddress(key.PublicKey).Hex(),
	}

	cfg.AccountAddress = getenv(EnvAccountAddress)
	if cfg.AccountAddress == "" {
		cfg.AccountAddress = cfg.WalletAddress
	}

	if cfg.Testnet {
		cfg.BaseURL = hyperliquid.TestnetAPIURL
	} else {
		cfg.BaseURL = hyperliquid.MainnetAPIURL
	}

	// HYPERLIQUID_BASE_URL (Go-only extension) overrides the network default —
	// e.g. to point at a proxy or a mock server.
	if override := strings.TrimSpace(getenv(EnvBaseURL)); override != "" {
		base, err := normalizeBaseURL(override)
		if err != nil {
			return Config{}, err
		}
		cfg.BaseURL = base
	}
	return cfg, nil
}

// normalizeBaseURL validates a HYPERLIQUID_BASE_URL value. The client appends
// "/info"/"/exchange" to it, so it must be an absolute http(s) URL without
// query or fragment; trailing slashes are dropped (a path prefix is kept, so
// proxied mounts like "https://host/hyperliquid" work). http is allowed for
// local mocks; whitespace is tolerated because, unlike EnvTestnet, this var
// has no Python parity constraint. The value is not secret, so the error
// echoes it for debuggability.
func normalizeBaseURL(raw string) (string, error) {
	s := strings.TrimRight(raw, "/")
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("invalid %s: must be an absolute http(s) URL, got %q", EnvBaseURL, raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("invalid %s: query/fragment not allowed, got %q", EnvBaseURL, raw)
	}
	return s, nil
}
