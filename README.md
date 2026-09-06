# hyperliquid-mcp-go

A [Model Context Protocol](https://modelcontextprotocol.io/) server for [Hyperliquid](https://hyperliquid.xyz/) perpetual trading, written in Go.

It is a drop-in replacement for the Python [`mcp-hyperliquid`](https://github.com/edkdev/hyperliquid-mcp) server: the same 23 tools and the same configuration, shipped as a single static binary — no Python runtime, no `uvx`/`pip`, fast cold start. An MCP client configured for the Python server switches over by changing only the `command` path. On top of that, it reads and trades HIP-3 builder perp DEXs (xyz, flx, vntl, …).

All order signing is delegated to the community SDK, [sonirico/go-hyperliquid](https://github.com/sonirico/go-hyperliquid). What remains here is MCP wiring and API calls — small enough to read end to end before you hand it a private key.

## Install

Requires Go 1.27+.

```bash
git clone https://github.com/evaleries/hyperliquid-mcp.git
cd hyperliquid-mcp
go install .        # installs hyperliquid-mcp-go into ~/go/bin
go test ./...       # optional: unit + mock-API tests, no network
```

The binary speaks MCP on stdio and logs to stderr.

## Configuration

Same environment variables as the Python version, plus one optional override:

| Variable | Required | Purpose |
| --- | --- | --- |
| `HYPERLIQUID_PRIVATE_KEY` | ✅ | Private key of the signing wallet |
| `HYPERLIQUID_ACCOUNT_ADDRESS` | ➖ | Agent/API-wallet mode: trading account address (defaults to the key-derived address) |
| `HYPERLIQUID_TESTNET` | ➖ | `"true"` for testnet; anything else or unset means mainnet |
| `HYPERLIQUID_VAULT_ADDRESS` | ➖ | Trade from a vault |
| `HYPERLIQUID_BASE_URL` | ➖ | Custom API endpoint (a proxy or a local mock); overrides the network default |

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

The same 23 tools as the Python server, plus 2 HIP-3 additions — 25 total.

**Account**

- `hyperliquid_get_account_info` — perpetual account summary: positions and margin
- `hyperliquid_get_positions` — open positions with margin summary
- `hyperliquid_get_balance` — account balance and withdrawable amount

**Orders**

- `hyperliquid_place_order` — place a single order (minimum value $10; use the asset index from `get_meta`)
- `hyperliquid_place_bracket_order` — entry + take-profit + stop-loss in one atomic batch
- `hyperliquid_cancel_order` — cancel an order by coin name and order ID (`oid`)
- `hyperliquid_cancel_all_orders` — cancel all open orders for the user
- `hyperliquid_modify_order` — modify an existing order
- `hyperliquid_place_twap_order`, `hyperliquid_cancel_twap_order` — stubs that always fail, as in the Python version

**Order queries**

- `hyperliquid_get_open_orders` — currently open orders
- `hyperliquid_get_order_status` — status of a specific order by `oid`
- `hyperliquid_get_user_fills` — historical trade fills
- `hyperliquid_get_user_funding` — funding payment history

**Market data**

- `hyperliquid_get_meta` — exchange metadata: asset indices, names, max leverage
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

**HIP-3**

- `hyperliquid_get_perp_dexs` — list builder-deployed perp DEXs
- `hyperliquid_get_dex_meta` — a builder DEX's asset universe (`dex` defaults to `xyz`; empty string selects the main DEX)

## Trading on HIP-3 builder DEXs

Builder DEXs use their own asset IDs: `assetIdBase` + universe index, both reported by `hyperliquid_get_dex_meta`. For xyz (`assetIdBase` 110000), the asset at universe index 11 has ID `110011`.

Pass that ID to `hyperliquid_place_order` or `hyperliquid_place_bracket_order` and the order lands on the builder DEX. Cancel and modify take the dex-prefixed coin name shown in your open orders (e.g. `xyz:CL`); `hyperliquid_cancel_all_orders` accepts a `dex` parameter.

## License

MIT (same as the Python original)
