// Package hl wraps the sonirico/go-hyperliquid SDK: construction, startup
// verification, raw /info posts, and the asset-index → coin-name mapping.
package hl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sonirico/go-hyperliquid"

	"github.com/edkdev/hyperliquid-mcp-go/internal/config"
)

// Client bundles the SDK handles and the resolved account context that tool
// handlers need.
type Client struct {
	// Info is the SDK read client. Used for typed needs only (meta universe
	// lookups); envelope-bound reads go through RawInfo for byte parity.
	Info *hyperliquid.Info
	// Exchange is the SDK signing client. All exchange actions go through it.
	Exchange *hyperliquid.Exchange

	// BaseURL of the Hyperliquid API (mainnet or testnet).
	BaseURL string
	// AccountAddress is the default account for userAddress parameters.
	AccountAddress string
	// VaultAddress is the configured vault ("" if none).
	VaultAddress string
	// WalletAddress is the signing (API wallet) address.
	WalletAddress string

	http *http.Client
}

// maxInfoResponseBytes caps RawInfo response bodies (security review
// SEC-DOS-002): a malfunctioning endpoint must not exhaust the process or
// flood the MCP client's context. 64 MiB is far above any real /info body.
// A var, not a const, so the boundary is testable without moving 64 MiB
// through a test server; nothing outside tests reassigns it.
var maxInfoResponseBytes int64 = 64 << 20

// hardenedHTTPClient is shared by RawInfo and the SDK (injected via options):
// redirects are refused (SEC-REDIRECT-001 — a cross-host 307/308 would
// otherwise re-POST bodies, including signed /exchange payloads) and a 60s
// timeout bounds every call.
//
// The transport is explicit because http.DefaultTransport keeps only 2 idle
// connections per host: mcp-go serves tool calls concurrently and every call
// targets the same API host, so a burst wider than 2 used to pay a fresh
// TCP+TLS handshake per extra call (measured 6.1 dials per 8-call burst;
// BenchmarkRawInfoConcurrent). 16 idle slots absorb realistic fan-out; all
// other defaults (HTTP/2, keep-alives) are preserved from the standard
// transport.
func hardenedHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 16
	return &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// New constructs SDK clients. The SDK constructors fetch meta/spotMeta
// eagerly and panic on failure; the panic is recovered into an error so main
// can exit cleanly with a stderr message.
func New(ctx context.Context, cfg config.Config) (client *Client, err error) {
	defer func() {
		if r := recover(); r != nil {
			client = nil
			err = fmt.Errorf("failed to initialize Hyperliquid SDK: %v", r)
		}
	}()

	httpClient := hardenedHTTPClient()
	clientOpt := hyperliquid.ClientOptHTTPClient(httpClient)

	// NewExchange builds its own Info internally (skipWS=true) and exposes it;
	// reusing it fetches meta/spotMeta exactly once at startup.
	exchange := hyperliquid.NewExchange(
		ctx, cfg.PrivateKey, cfg.BaseURL, nil, cfg.VaultAddress, cfg.AccountAddress, nil, nil,
		hyperliquid.ExchangeOptClientOptions(clientOpt),
		hyperliquid.ExchangeOptInfoOptions(hyperliquid.InfoOptClientOptions(clientOpt)),
	)
	info := exchange.Info()

	return &Client{
		Info:           info,
		Exchange:       exchange,
		BaseURL:        cfg.BaseURL,
		AccountAddress: cfg.AccountAddress,
		VaultAddress:   cfg.VaultAddress,
		WalletAddress:  cfg.WalletAddress,
		http:           httpClient,
	}, nil
}

// RawInfo performs an unsigned POST {baseURL}/info with the given payload and
// returns the response body untouched. This mirrors the Python SDK's
// Info.post: handlers pass the body through as the envelope's data field,
// preserving every field the API returns.
func (c *Client) RawInfo(ctx context.Context, payload map[string]any) (json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode info request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/info", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("info request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxInfoResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read info response: %w", err)
	}
	if int64(len(respBody)) > maxInfoResponseBytes {
		return nil, fmt.Errorf("info response exceeds %d bytes", maxInfoResponseBytes)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("info request failed with status %d: %s", resp.StatusCode, truncate(respBody, 256))
	}
	return json.RawMessage(respBody), nil
}

// VerifyWallet performs the startup wallet check (mirrors Python
// _init_hyperliquid): returns the account value on success. Callers log a
// warning on failure; it is never fatal.
func (c *Client) VerifyWallet(ctx context.Context) (string, error) {
	state, err := c.Info.UserState(ctx, c.AccountAddress)
	if err != nil {
		return "", err
	}
	return state.MarginSummary.AccountValue, nil
}

// CoinForAsset maps an asset index to a coin name via a fresh meta fetch
// (parity baseline: Python calls info.meta() per order).
func (c *Client) CoinForAsset(ctx context.Context, asset int64) (string, error) {
	meta, err := c.Info.Meta(ctx)
	if err != nil {
		return "", err
	}
	if asset < 0 || asset >= int64(len(meta.Universe)) {
		return "", fmt.Errorf("invalid asset index %d: meta universe has %d assets", asset, len(meta.Universe))
	}
	return meta.Universe[asset].Name, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
