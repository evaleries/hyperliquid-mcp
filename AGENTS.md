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

1. **Parity first, on the MCP surface.** Tool names, descriptions, JSON schemas, env
   vars, and response envelopes must match the Python reference (rule 4); the golden
   schema test is the gate and it fails, never skips. Behavior parity stops where the
   reference is broken: seven of its 23 tools raise `AttributeError`/`TypeError`
   before reaching the API (missing or misnamed SDK methods). Those are implemented
   for real here — do not "restore parity" by regressing them. Every intentional
   difference is recorded in README's "Divergences from the Python reference"; add to
   that table instead of narrating divergences in code comments.
2. **stdout is sacred.** The MCP protocol runs on stdout. All logs go to stderr,
   always (`log.SetOutput(os.Stderr)` or an stderr slog handler). Never `fmt.Println`.
3. **No secrets in code, tests, or fixtures.** Integration tests are env-gated.
4. Reference implementation for behavior questions:
   `~/workspace/hyperliquid-mcp/src/hyperliquid_mcp/server.py` when checked out
   locally (read-only; do not modify that repo from here), otherwise
   `edkdev/hyperliquid-mcp` @ `7f39651` — the commit the golden fixture was extracted
   from. Claims about the Python SDK must be checked against
   `hyperliquid-dex/hyperliquid-python-sdk` (verified at tag `0.24.0`; the reference
   pins only `>=0.6.0`).

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

- [x] All 23 tools implemented at schema parity (golden test green)
- [x] Golden schema test passes — fixture `internal/tools/testdata/tools.python.json`
  is **tracked** (static literals from the reference, no secrets) and enforced by CI;
  regenerate by AST-extracting `list_tools()`'s `Tool(...)` literals from the Python
  reference into `[{name, description, inputSchema}]`. Keep `.gitignore` patterns for
  local-only dirs root-anchored so nested `testdata/` stays tracked.
- [ ] Manual testnet smoke test: place + cancel an order
- [ ] `go build` produces a static binary that runs an MCP session over stdio
