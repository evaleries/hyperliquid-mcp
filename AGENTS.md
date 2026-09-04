# AGENTS.md

## What this is

Go rewrite of the Python [`mcp-hyperliquid`](https://github.com/edkdev/hyperliquid-mcp)
MCP server (Hyperliquid perpetuals trading over MCP stdio). **Implemented at parity**
(2026-09-03): all 23 tools, golden schema test green, stdio smoke-tested against mainnet.

## Locked decisions

- MCP framework: `github.com/mark3labs/mcp-go` v1.x — do not swap.
- Hyperliquid SDK: `github.com/sonirico/go-hyperliquid` v0.44+ — do not hand-roll
  signing/wire-format code.
- `Info.RecentTrades` does not exist in that SDK — use the raw `POST /info` helper
  (`hl.Client.RawInfo`).
- TWAP tools are intentionally unimplemented stubs (parity with Python).
- Endpoint override: `HYPERLIQUID_BASE_URL` env var replaces the testnet-derived base
  URL when set (Go-only extension, no Python counterpart).

## Hard rules

1. **Parity first.** Tool names, JSON schemas, env vars, and response envelopes must match
   the Python reference (rule 4); the golden schema test is the gate.
2. **stdout is sacred.** The MCP protocol runs on stdout. All logs go to stderr,
   always (`log.SetOutput(os.Stderr)` or an stderr slog handler). Never `fmt.Println`.
3. **No secrets in code, tests, or fixtures.** Integration tests are env-gated.
4. Reference implementation for behavior questions:
   `~/workspace/hyperliquid-mcp/src/hyperliquid_mcp/server.py` (read-only; do not modify
   that repo from here).

## Environment

- Go 1.27.1 is installed at `/usr/local/go` (added 2026-09-03); use
  `export PATH=$PATH:/usr/local/go/bin`.
- Repo lives at `~/workspace/hyperliquid-mcp-go`; canonical remote
  `https://github.com/evaleries/hyperliquid-mcp` (private).
- Git identity in this repo is `evaleries` (repo-local config) — do not change it;
  force-pushing rewritten history to `main` is accepted practice here (owner's call).

## Commands

```bash
go build ./...        # build
go test ./...         # unit + httptest layers
go vet ./...          # lint baseline
go test -tags=integration ./...   # testnet smoke test (needs env secrets; manual only)
```

## Definition of done for the parity release

- [ ] All 23 tools implemented at schema parity (golden test green)
- [ ] Golden schema test passes (fixture `internal/tools/testdata/tools.python.json`
  is local-only/gitignored, regenerable from the Python reference; tests skip without it)
- [ ] Manual testnet smoke test: place + cancel an order
- [ ] `go build` produces a static binary that runs an MCP session over stdio
