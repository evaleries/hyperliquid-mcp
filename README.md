# hyperliquid-mcp-go

A [Model Context Protocol](https://modelcontextprotocol.io/) server for [Hyperliquid](https://hyperliquid.xyz/) perpetual trading, written in Go.

This is a Go rewrite of the Python [`mcp-hyperliquid`](https://github.com/edkdev/hyperliquid-mcp) server. It gives AI assistants (Claude Desktop, Kiro, and other MCP clients) secure access to Hyperliquid's trading API over stdio.

> **Status:** Parity release implemented — all 23 tools from the Python
> reference are shipped, schemas verified against it by a golden test —
> **plus HIP-3 extensions**: builder perp DEX discovery and dex-qualified meta
> (`hyperliquid_get_perp_dexs`, `hyperliquid_get_dex_meta`). Remaining before
> tagging: manual testnet smoke test (`go test -tags=integration ./...` with a
> funded testnet key).

## Build & run

Requires Go 1.27+.

```bash
go build -o hyperliquid-mcp-go .   # produces the server binary
go install .                       # or install to ~/go/bin for PATH-wide use
go test ./...                      # unit + httptest layers (no network)
go test ./... -bench . -run '^$'   # perf guards (startup, JSON pipeline, pooling)
go vet ./...                       # lint baseline
# manual, needs a funded testnet key in HYPERLIQUID_PRIVATE_KEY:
go test -tags=integration ./internal/tools/ -run Integration -v
```

The binary speaks MCP on stdio; logs go to stderr. Configure your MCP client
with the environment variables below (same as the Python version).

## Why Go

The Python original requires a Python 3.10+ runtime and `uvx`/`pip` to run. The Go rewrite ships as:

- **A single static binary** — no runtime, no dependency resolution at startup, fast cold start
- **Cross-compiled releases** — one binary per OS/arch, trivially installable
- **The same MCP contract** — identical tool names, schemas, and configuration, so existing MCP client configs keep working with a one-line change (the command path)

## Goals

1. **Feature parity with `mcp-hyperliquid` v0.1.0** — all 23 tools, same names, same input schemas, same environment variables, same response shapes.
2. **Drop-in replacement** — an MCP client configured for the Python server can switch to the Go binary by changing only `command`/`args`.
3. **Thin, auditable core** — all signing and wire-format concerns delegated to the community SDK; this repo contains only MCP wiring and Hyperliquid API calls.

## Technology choices (decided)

| Concern | Choice | Version |
| --- | --- | --- |
| MCP framework | [`github.com/mark3labs/mcp-go`](https://pkg.go.dev/github.com/mark3labs/mcp-go) | v1.x |
| Hyperliquid SDK | [`github.com/sonirico/go-hyperliquid`](https://pkg.go.dev/github.com/sonirico/go-hyperliquid) | v0.44+ |
| JSON engine | [`bytedance/sonic`](https://github.com/bytedance/sonic) for envelopes, stdlib for streaming decode | v1.x |
| Transport | stdio (same as Python) | — |
| Distribution | `go install` + prebuilt binaries (goreleaser, later) | — |

## Configuration

Environment variables (the first four match the Python version; `HYPERLIQUID_BASE_URL` is a Go-only extension):

| Variable | Required | Purpose |
| --- | --- | --- |
| `HYPERLIQUID_PRIVATE_KEY` | ✅ | Private key of the signing wallet |
| `HYPERLIQUID_ACCOUNT_ADDRESS` | ➖ | Agent/API-wallet mode: trading account address (defaults to key-derived address) |
| `HYPERLIQUID_TESTNET` | ➖ | `"true"` for testnet, anything else/unset = mainnet |
| `HYPERLIQUID_VAULT_ADDRESS` | ➖ | Trade from a vault |
| `HYPERLIQUID_BASE_URL` | ➖ | Custom API endpoint (e.g. `https://your-proxy.example.com` or `http://localhost:8080` for a mock); overrides the mainnet/testnet default implied by `HYPERLIQUID_TESTNET` |

Example MCP client config:

```json
{
  "mcpServers": {
    "hyperliquid": {
      "command": "hyperliquid-mcp-go",
      "args": [],
      "env": {
        "HYPERLIQUID_PRIVATE_KEY": "0x1234567890abcdef...",
        "HYPERLIQUID_TESTNET": "false"
      }
    }
  }
}
```

## Available tools

All 23 tools of the Python server (same names, input schemas, and response shapes), plus 2 HIP-3 read extensions — 25 total.

**Account**

- `hyperliquid_get_account_info` — perpetual account summary: positions and margin
- `hyperliquid_get_positions` — open positions with margin summary
- `hyperliquid_get_balance` — account balance and withdrawable amount

**Orders**

- `hyperliquid_place_order` — place a single order (minimum value $10; use asset index from `get_meta`)
- `hyperliquid_place_bracket_order` — entry + take-profit + stop-loss in one atomic batch (TP/SL auto reduce-only triggers)
- `hyperliquid_cancel_order` — cancel a specific order by coin name and order ID (`oid`)
- `hyperliquid_cancel_all_orders` — cancel all open orders for the user
- `hyperliquid_modify_order` — modify an existing order
- `hyperliquid_place_twap_order`, `hyperliquid_cancel_twap_order` — listed for parity; both are stubs that always fail, same as the Python reference

**Order queries**

- `hyperliquid_get_open_orders` — currently open orders
- `hyperliquid_get_order_status` — status of a specific order by `oid`
- `hyperliquid_get_user_fills` — historical trade fills
- `hyperliquid_get_user_funding` — funding payment history

**Market data**

- `hyperliquid_get_meta` — exchange metadata: asset indices, names, max leverage (maps coin names to asset indices)
- `hyperliquid_get_all_mids` — current mid prices for all assets
- `hyperliquid_get_order_book` — L2 order book (market depth) for an asset
- `hyperliquid_get_recent_trades` — recent trades for an asset
- `hyperliquid_get_historical_funding` — historical funding rates for an asset
- `hyperliquid_get_candles` — candle/OHLCV data for an asset

**Vaults**

- `hyperliquid_vault_details` — detailed information about a specific vault
- `hyperliquid_vault_performance` — performance metrics for a specific vault

**Utility**

- `hyperliquid_get_server_time` — estimated server time

**HIP-3 extensions** (beyond parity — details below)

- `hyperliquid_get_perp_dexs` — list builder-deployed perp DEXs with their `perpDexIndex`
- `hyperliquid_get_dex_meta` — a builder DEX's asset universe (`dex` defaults to `xyz`)

## HIP-3 builder perp DEXs (extension)

Beyond parity: read-only access to Hyperliquid's
[HIP-3](https://hyperliquid.gitbook.io/hyperliquid-docs/hyperliquid-improvement-proposals-hips/hip-3-builder-deployed-perpetuals)
builder-deployed perp DEXs:

- `hyperliquid_get_perp_dexs` — list builder DEXs (xyz, flx, vntl, …) with their `perpDexIndex`
- `hyperliquid_get_dex_meta` — a DEX's asset universe; `dex` defaults to `xyz`, empty string = main DEX

Example: *"What can I trade on the xyz DEX?"* → the model calls
`hyperliquid_get_dex_meta` and gets the universe plus `assetIdBase`
(`100000 + perpDexIndex × 10000`) for constructing builder asset IDs.

Trading on builder DEXs (hip3-trade) and HIP-4 outcome-market reads are
planned modules.

## Documentation

- [AGENTS.md](AGENTS.md) — working agreements for coding agents in this repo

## License

MIT (same as the Python original)
