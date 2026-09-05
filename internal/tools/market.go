package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/server"

	"github.com/edkdev/hyperliquid-mcp-go/internal/hl"
)

// Market Data (server.py §4). All six tools are raw /info reads: the API
// response passes through untouched as data.

func marketTools(c *hl.Client) []server.ServerTool {
	return []server.ServerTool{
		tool(
			"hyperliquid_get_meta",
			"Get exchange metadata including all available trading assets with their indices, names, max leverage, and trading parameters. Essential for mapping coin names to asset indices.",
			schema(map[string]any{}),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return getMeta(ctx, c, args)
			},
		),
		tool(
			"hyperliquid_get_all_mids",
			"Get current mid prices for all assets",
			schema(map[string]any{}),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return getAllMids(ctx, c, args)
			},
		),
		tool(
			"hyperliquid_get_order_book",
			"Get order book (market depth) for a specific asset",
			schema(map[string]any{
				"coin": strProp("Asset symbol (e.g., 'BTC', 'ETH', 'SOL')"),
			}, "coin"),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return getOrderBook(ctx, c, args)
			},
		),
		tool(
			"hyperliquid_get_recent_trades",
			"Get recent trades for a specific asset",
			schema(map[string]any{
				"coin": strProp("Asset symbol (e.g., 'BTC', 'ETH', 'SOL')"),
			}, "coin"),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return getRecentTrades(ctx, c, args)
			},
		),
		tool(
			"hyperliquid_get_historical_funding",
			"Get historical funding rates for an asset",
			schema(map[string]any{
				"coin":      strProp("Asset symbol (e.g., 'BTC', 'ETH', 'SOL')"),
				"startTime": intProp("Start time in milliseconds", nil),
				"endTime":   intProp("End time in milliseconds (optional, defaults to current time)", nil),
			}, "coin", "startTime"),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return getHistoricalFunding(ctx, c, args)
			},
		),
		tool(
			"hyperliquid_get_candles",
			"Get historical candle/OHLCV data for an asset",
			schema(map[string]any{
				"coin": strProp("Asset symbol (e.g., 'BTC', 'ETH', 'SOL')"),
				"interval": map[string]any{
					"type":        "string",
					"description": "Candle interval",
					"enum":        []string{"1m", "5m", "15m", "1h", "4h", "1d"},
				},
				"startTime": intProp("Start time in milliseconds", nil),
				"endTime":   intProp("End time in milliseconds (optional, defaults to current time)", nil),
			}, "coin", "interval", "startTime"),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return getCandles(ctx, c, args)
			},
		),
	}
}

// Info.meta and Info.all_mids both send "dex" (empty for the main perp dex).

func getMeta(ctx context.Context, c *hl.Client, _ map[string]any) (map[string]any, error) {
	raw, err := c.RawInfo(ctx, map[string]any{
		"type": "meta",
		"dex":  "",
	})
	if err != nil {
		return nil, err
	}
	assets, err := universeAssets(raw)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"message": "Exchange metadata retrieved successfully",
		"data":    raw,
		"summary": map[string]any{
			"numberOfAssets":    len(assets),
			"assetsWithIndices": assets,
		},
	}, nil
}

// universeAssets enumerates a meta response body like the Python list
// comprehension: index, name, maxLeverage, onlyIsolated per universe entry,
// with onlyIsolated defaulting to false when the key is absent. Shared by
// getMeta (main DEX) and getDexMeta (HIP-3 builder DEXs).
func universeAssets(raw json.RawMessage) ([]any, error) {
	result, err := rawToMap(raw)
	if err != nil {
		return nil, err
	}
	universe, ok := result["universe"].([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected response: missing universe")
	}
	assets := make([]any, len(universe))
	for i, u := range universe {
		asset, ok := u.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("unexpected response: universe entry %d is not an object", i)
		}
		onlyIsolated, ok := asset["onlyIsolated"]
		if !ok {
			onlyIsolated = false
		}
		assets[i] = map[string]any{
			"index":        i,
			"name":         asset["name"],
			"maxLeverage":  asset["maxLeverage"],
			"onlyIsolated": onlyIsolated,
		}
	}
	return assets, nil
}

func getAllMids(ctx context.Context, c *hl.Client, _ map[string]any) (map[string]any, error) {
	raw, err := c.RawInfo(ctx, map[string]any{
		"type": "allMids",
		"dex":  "",
	})
	if err != nil {
		return nil, err
	}
	result, err := rawToMap(raw)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"message": "All mid prices retrieved successfully",
		"data":    raw,
		"summary": map[string]any{
			"numberOfAssets": len(result),
		},
	}, nil
}

func getOrderBook(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	coin, err := RequireString(args, "coin")
	if err != nil {
		return nil, err
	}
	raw, err := c.RawInfo(ctx, map[string]any{
		"type": "l2Book",
		"coin": coin,
	})
	if err != nil {
		return nil, err
	}
	result, err := rawToMap(raw)
	if err != nil {
		return nil, err
	}
	// Python: len(result["levels"][0]) if result.get("levels") else 0.
	bidsCount, asksCount := 0, 0
	if levels, ok := result["levels"].([]any); ok && len(levels) >= 2 {
		bidsCount = lenOf(levels[0])
		asksCount = lenOf(levels[1])
	}
	return map[string]any{
		"message": fmt.Sprintf("Order book for %s retrieved successfully", coin),
		"data":    raw,
		"summary": map[string]any{
			"coin":      coin,
			"bidsCount": bidsCount,
			"asksCount": asksCount,
		},
	}, nil
}

// lenOf is len for a decoded JSON array, 0 for anything else.
func lenOf(v any) int {
	if arr, ok := v.([]any); ok {
		return len(arr)
	}
	return 0
}

// getRecentTrades has no working reference behavior to match: the Python
// server calls Info.recent_trades, which the Python SDK does not define, so
// the tool raises AttributeError there. The wire body is the API's own
// recentTrades query. See README divergences.
func getRecentTrades(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	coin, err := RequireString(args, "coin")
	if err != nil {
		return nil, err
	}
	raw, err := c.RawInfo(ctx, map[string]any{
		"type": "recentTrades",
		"coin": coin,
	})
	if err != nil {
		return nil, err
	}
	trades, err := rawToSlice(raw)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"message": fmt.Sprintf("Recent trades for %s retrieved successfully", coin),
		"data":    raw,
		"summary": map[string]any{
			"coin":           coin,
			"numberOfTrades": len(trades),
		},
	}, nil
}

// marketEndTime reads the optional endTime with Python truthiness: absent,
// null, or 0 all mean "not provided" (server.py uses
// `int(arguments["endTime"]) if arguments.get("endTime") else None` for the
// funding/candles handlers). Post-normalization the value is int64.
func marketEndTime(args map[string]any) (int64, bool) {
	v, ok := args["endTime"]
	if !ok || v == nil {
		return 0, false
	}
	n, err := coerceInt64(v)
	if err != nil || n == 0 {
		return 0, false
	}
	return n, true
}

func getHistoricalFunding(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	coin, err := RequireString(args, "coin")
	if err != nil {
		return nil, err
	}
	startTime, err := RequireInt(args, "startTime")
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"type":      "fundingHistory",
		"coin":      coin,
		"startTime": startTime,
	}
	// fundingHistory carries endTime only when provided, and a falsy endTime
	// (0) is treated as absent — the reference server's
	// `int(arguments["endTime"]) if arguments.get("endTime") else None`
	// intent, kept because the schema documents endTime as optional.
	if endTime, ok := marketEndTime(args); ok {
		body["endTime"] = endTime
	}
	raw, err := c.RawInfo(ctx, body)
	if err != nil {
		return nil, err
	}
	numberOfEntries, err := rawArrayLen(raw)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"message": fmt.Sprintf("Historical funding for %s retrieved successfully", coin),
		"data":    raw,
		"summary": map[string]any{
			"coin":            coin,
			"numberOfEntries": numberOfEntries,
		},
	}, nil
}

func getCandles(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	coin, err := RequireString(args, "coin")
	if err != nil {
		return nil, err
	}
	interval, err := RequireString(args, "interval")
	if err != nil {
		return nil, err
	}
	startTime, err := RequireInt(args, "startTime")
	if err != nil {
		return nil, err
	}
	// candleSnapshot always carries the endTime key, null when omitted.
	// Same falsy→None remap as getHistoricalFunding.
	var endTime any
	if v, ok := marketEndTime(args); ok {
		endTime = v
	}
	raw, err := c.RawInfo(ctx, map[string]any{
		"type": "candleSnapshot",
		"req": map[string]any{
			"coin":      coin,
			"interval":  interval,
			"startTime": startTime,
			"endTime":   endTime,
		},
	})
	if err != nil {
		return nil, err
	}
	numberOfCandles, err := rawArrayLen(raw)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"message": fmt.Sprintf("Candles for %s (%s) retrieved successfully", coin, interval),
		"data":    raw,
		"summary": map[string]any{
			"coin":            coin,
			"interval":        interval,
			"numberOfCandles": numberOfCandles,
		},
	}, nil
}
