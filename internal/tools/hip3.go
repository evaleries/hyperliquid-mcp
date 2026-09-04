package tools

// HIP-3 builder-deployed perp DEX reads.
// Post-parity extension: raw /info passthrough only, no SDK calls.
// The perpDexs array index doubles as the perpDexIndex in builder asset-ID
// math (asset = 100000 + perpDexIndex*10000 + indexInUniverse) — hip3-trade
// depends on resolvePerpDex for that.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/server"

	"github.com/edkdev/hyperliquid-mcp-go/internal/hl"
)

// defaultPerpDex is the initiative-locked default for HIP-3 tools.
// An explicit empty dex still selects the main DEX.
const defaultPerpDex = "xyz"

func hip3Tools(c *hl.Client) []server.ServerTool {
	return []server.ServerTool{
		tool(
			"hyperliquid_get_perp_dexs",
			"List HIP-3 builder-deployed perpetual DEXs. The response array's index 0 is the main DEX (null); builder DEXs follow. The array index is the perpDexIndex used in builder asset IDs.",
			schema(map[string]any{}),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return getPerpDexs(ctx, c, args)
			},
		),
		tool(
			"hyperliquid_get_dex_meta",
			"Get the asset universe of a HIP-3 builder perp DEX (default: xyz; empty string = main DEX). The summary carries perpDexIndex and assetIdBase for constructing builder asset IDs (assetIdBase + universe index; builder DEXs only — main-DEX asset IDs are bare universe indices).",
			schema(map[string]any{
				"dex": strPropDefault("Perp dex name (e.g. xyz). Empty string selects the main perp DEX.", defaultPerpDex),
			}),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return getDexMeta(ctx, c, args)
			},
		),
	}
}

func getPerpDexs(ctx context.Context, c *hl.Client, _ map[string]any) (map[string]any, error) {
	raw, err := c.RawInfo(ctx, map[string]any{"type": "perpDexs"})
	if err != nil {
		return nil, err
	}
	entries, err := decodePerpDexs(raw)
	if err != nil {
		return nil, err
	}
	list := make([]any, 0, len(entries))
	for _, e := range entries {
		list = append(list, map[string]any{
			"index":    e.index,
			"name":     e.info["name"],
			"fullName": e.info["fullName"],
			"deployer": e.info["deployer"],
		})
	}
	return map[string]any{
		"message": fmt.Sprintf("Retrieved %d builder perp DEXs", len(list)),
		"data":    raw,
		"summary": map[string]any{
			"numberOfDexs": len(list),
			"dexs":         list,
		},
	}, nil
}

// perpDexEntry is one builder DEX from the perpDexs response: its array
// index (the perpDexIndex of builder asset-ID math) and its decoded object.
type perpDexEntry struct {
	index int
	info  map[string]any
}

// decodePerpDexs validates a perpDexs response body and returns the non-null
// entries with their array indices. Index 0 is the null main-DEX slot and is
// skipped. Shared by getPerpDexs (summary) and resolvePerpDex (lookup).
func decodePerpDexs(raw json.RawMessage) ([]perpDexEntry, error) {
	dexs, err := rawToSlice(raw)
	if err != nil {
		return nil, err
	}
	entries := make([]perpDexEntry, 0, len(dexs))
	for i, d := range dexs {
		if d == nil {
			continue
		}
		entry, ok := d.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("unexpected response: perpDexs entry %d is not an object", i)
		}
		entries = append(entries, perpDexEntry{index: i, info: entry})
	}
	return entries, nil
}

func getDexMeta(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	dex := OptString(args, "dex", defaultPerpDex)

	// The dex-index resolution and the meta fetch are independent calls; run
	// them concurrently to save one round trip per invocation (~150-225ms on
	// the live API). Error precedence matches the former sequential version:
	// resolution errors win over meta errors. The main DEX (dex == "") needs
	// no resolution, so it stays a single call.
	type indexResult struct {
		index int
		err   error
	}
	type metaResult struct {
		raw json.RawMessage
		err error
	}
	idxCh := make(chan indexResult, 1)
	metaCh := make(chan metaResult, 1)
	go func() {
		idx, err := resolvePerpDex(ctx, c, dex)
		idxCh <- indexResult{idx, err}
	}()
	go func() {
		raw, err := c.RawInfo(ctx, map[string]any{"type": "meta", "dex": dex})
		metaCh <- metaResult{raw, err}
	}()
	idxRes, metaRes := <-idxCh, <-metaCh
	if idxRes.err != nil {
		return nil, idxRes.err
	}
	if metaRes.err != nil {
		return nil, metaRes.err
	}
	perpDexIndex, raw := idxRes.index, metaRes.raw
	// Enumerate exactly like getMeta (parity shape: raw in data, the
	// enumeration in summary.assetsWithIndices).
	assets, err := universeAssets(raw)
	if err != nil {
		return nil, err
	}
	label := dex
	if label == "" {
		label = "main"
	}
	return map[string]any{
		"message": fmt.Sprintf("Meta for perp dex %s retrieved successfully", label),
		"data":    raw,
		"summary": map[string]any{
			"dex":               dex,
			"perpDexIndex":      perpDexIndex,
			"assetIdBase":       100000 + perpDexIndex*10000,
			"numberOfAssets":    len(assets),
			"assetsWithIndices": assets,
		},
	}, nil
}

// resolvePerpDex maps a builder DEX name to its perpDexs array index via one
// unsigned /info call (deliberately uncached for freshness). The main
// DEX ("") is index 0.
func resolvePerpDex(ctx context.Context, c *hl.Client, dex string) (int, error) {
	if dex == "" {
		return 0, nil
	}
	raw, err := c.RawInfo(ctx, map[string]any{"type": "perpDexs"})
	if err != nil {
		return 0, err
	}
	entries, err := decodePerpDexs(raw)
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		if e.info["name"] == dex {
			return e.index, nil
		}
	}
	return 0, fmt.Errorf("unknown perp dex %q (not in perpDexs)", dex)
}
