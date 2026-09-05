package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/server"

	"github.com/edkdev/hyperliquid-mcp-go/internal/hl"
)

// Order Queries (server.py §3). All four tools are raw /info reads:
// the response body passes through untouched as the envelope's data field.

func queryTools(c *hl.Client) []server.ServerTool {
	return []server.ServerTool{
		tool(
			"hyperliquid_get_open_orders",
			"Get user's currently open orders",
			schema(map[string]any{
				"userAddress": strProp("User address (optional, defaults to configured account)"),
				"dex":         strPropDefault("Perp dex name (optional)", ""),
			}),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return getOpenOrders(ctx, c, args)
			},
		),
		tool(
			"hyperliquid_get_order_status",
			"Get the status of a specific order by oid",
			schema(map[string]any{
				"userAddress": strProp("User address (optional, defaults to configured account)"),
				"oid":         intProp("Order ID", nil),
			}, "oid"),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return getOrderStatus(ctx, c, args)
			},
		),
		tool(
			"hyperliquid_get_user_fills",
			"Get user's historical trade fills",
			schema(map[string]any{
				"userAddress":     strProp("User address (optional, defaults to configured account)"),
				"startTime":       intProp("Start time in milliseconds (required)", nil),
				"endTime":         intProp("End time in milliseconds (optional, defaults to current time)", nil),
				"aggregateByTime": boolPropDefault("Whether to aggregate partial fills by time", false),
			}, "startTime"),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return getUserFills(ctx, c, args)
			},
		),
		tool(
			"hyperliquid_get_user_funding",
			"Get user's funding payment history",
			schema(map[string]any{
				"userAddress": strProp("User address (optional, defaults to configured account)"),
				"startTime":   intProp("Start time in milliseconds (required)", nil),
				"endTime":     intProp("End time in milliseconds (optional, defaults to current time)", nil),
			}, "startTime"),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return getUserFunding(ctx, c, args)
			},
		),
	}
}

// getOpenOrders posts the openOrders query. Info.open_orders always includes
// the "dex" key, even when empty.
func getOpenOrders(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	raw, err := c.RawInfo(ctx, map[string]any{
		"type": "openOrders",
		"user": UserAddress(args, c),
		"dex":  Dex(args),
	})
	if err != nil {
		return nil, err
	}
	orders, err := rawToSlice(raw)
	if err != nil {
		return nil, err
	}
	// Python: len(result) if result else 0 — rawToSlice maps JSON null to a
	// nil slice, so len covers both.
	return map[string]any{
		"message": "Open orders retrieved successfully",
		"data":    raw,
		"summary": map[string]any{
			"numberOfOrders": len(orders),
		},
	}, nil
}

func getOrderStatus(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	oid, err := RequireInt(args, "oid")
	if err != nil {
		return nil, err
	}
	raw, err := c.RawInfo(ctx, map[string]any{
		"type": "orderStatus",
		"user": UserAddress(args, c),
		"oid":  oid,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"message": "Order status retrieved successfully",
		"data":    raw,
		"orderId": oid,
	}, nil
}

func getUserFills(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	startTime, err := RequireInt(args, "startTime")
	if err != nil {
		return nil, err
	}
	endTime, hasEndTime := queryEndTimeParam(args)
	// Info.user_fills_by_time's body always carries "endTime", null when
	// omitted (the reference server never reaches it — it calls that method
	// with a "user=" keyword it does not accept; see README divergences).
	var endWire any
	if hasEndTime {
		endWire = endTime
	}
	raw, err := c.RawInfo(ctx, map[string]any{
		"type":            "userFillsByTime",
		"user":            UserAddress(args, c),
		"startTime":       startTime,
		"endTime":         endWire,
		"aggregateByTime": OptBool(args, "aggregateByTime", false),
	})
	if err != nil {
		return nil, err
	}
	numberOfFills, err := rawArrayLen(raw)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"message": "User fills retrieved successfully",
		"data":    raw,
		"summary": map[string]any{
			"numberOfFills": numberOfFills,
			"timeRange":     queryTimeRange(startTime, endTime, hasEndTime),
		},
	}, nil
}

func getUserFunding(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	startTime, err := RequireInt(args, "startTime")
	if err != nil {
		return nil, err
	}
	endTime, hasEndTime := queryEndTimeParam(args)
	// Info.user_funding_history's body carries "endTime" only when provided,
	// never a null key (the reference server calls a nonexistent
	// Info.user_funding; see README divergences).
	body := map[string]any{
		"type":      "userFunding",
		"user":      UserAddress(args, c),
		"startTime": startTime,
	}
	if hasEndTime {
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
		"message": "User funding retrieved successfully",
		"data":    raw,
		"summary": map[string]any{
			"numberOfEntries": numberOfEntries,
			"timeRange":       queryTimeRange(startTime, endTime, hasEndTime),
		},
	}, nil
}

// queryEndTimeParam reads the optional endTime parameter. Integer
// normalization (wrap) has already coerced a present endTime to int64; an
// absent or explicit-null endTime reports ok=false.
func queryEndTimeParam(args map[string]any) (endTime int64, ok bool) {
	switch v := args["endTime"].(type) {
	case nil:
		return 0, false
	case int64:
		return v, true
	default:
		n, err := coerceInt64(v)
		if err != nil {
			return 0, false
		}
		return n, true
	}
}

// queryTimeRange renders the summary time range; an absent or zero endTime
// renders as the string "current" (Python `end_time or "current"`).
func queryTimeRange(startTime, endTime int64, hasEndTime bool) map[string]any {
	end := any("current")
	if hasEndTime && endTime != 0 {
		end = endTime
	}
	return map[string]any{"startTime": startTime, "endTime": end}
}
