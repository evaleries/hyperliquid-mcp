package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/server"
	"github.com/sonirico/go-hyperliquid"

	"github.com/edkdev/hyperliquid-mcp-go/internal/hl"
)

// Order Management (server.py §2). All signing paths go through the SDK;
// exchange envelopes are rebuilt as Python-shaped maps; SDK error promotion
// is suppressed when an HTTP response exists. TWAP tools remain listed but
// always fail.

func orderTools(c *hl.Client) []server.ServerTool {
	return []server.ServerTool{
		tool(
			"hyperliquid_place_order",
			"Place a single order on Hyperliquid. Minimum order value is $10. Use asset index from get_meta (e.g., 0=BTC, 1=ETH, 5=SOL).",
			schema(map[string]any{
				"asset":      intProp("Asset index (e.g., 0 for BTC, 1 for ETH, 5 for SOL). Use hyperliquid_get_meta to get the full list.", int64ptr(0)),
				"isBuy":      map[string]any{"type": "boolean", "description": "True for buy/long orders, false for sell/short orders"},
				"size":       strProp("Order size/quantity as a string (e.g., '0.1' for 0.1 BTC). Ensure size * price >= $10."),
				"price":      strProp("Limit price as a string (e.g., '181.5'). Set to '0' for market orders."),
				"reduceOnly": boolPropDefault("Whether this is a reduce-only order (only closes existing positions)", false),
				"orderType": objPropDefault(
					"Order type configuration. For limit orders use {limit: {tif: 'Gtc'}}. For trigger orders use {trigger: {isMarket: false, triggerPx: 'price', tpsl: 'tp' or 'sl'}}",
					map[string]any{"limit": map[string]any{"tif": "Gtc"}},
				),
				"cloid": strProp("Client order ID (optional, for tracking)"),
			}, "asset", "isBuy", "size"),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return placeOrder(ctx, c, args)
			},
		),
		tool(
			"hyperliquid_place_bracket_order",
			"Place a complete bracket order (entry + take profit + stop loss) in a single atomic batch. Minimum order value is $10. The TP and SL orders are automatically set as reduce-only and trigger orders.",
			schema(map[string]any{
				"asset":           intProp("Asset index (e.g., 0 for BTC, 1 for ETH, 5 for SOL)", int64ptr(0)),
				"isBuy":           map[string]any{"type": "boolean", "description": "True for buy/long positions, false for sell/short positions"},
				"size":            strProp("Position size as a string (e.g., '4.96' for 4.96 SOL)"),
				"entryPrice":      strProp("Entry limit price as a string (e.g., '181.5'). Set to '0' for market entry."),
				"takeProfitPrice": strProp("Take profit trigger price. For long: above entry. For short: below entry."),
				"stopLossPrice":   strProp("Stop loss trigger price. For long: below entry. For short: above entry."),
				"reduceOnly":      boolPropDefault("Whether the ENTRY order is reduce-only (usually false)", false),
				"entryOrderType":  objPropDefault("Entry order type configuration", map[string]any{"limit": map[string]any{"tif": "Gtc"}}),
			}, "asset", "isBuy", "size", "takeProfitPrice", "stopLossPrice"),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return placeBracketOrder(ctx, c, args)
			},
		),
		tool(
			"hyperliquid_cancel_order",
			"Cancel a specific order by coin name and order ID (oid). Always use oid for cancellation.",
			schema(map[string]any{
				"coin": strProp("Coin/asset name (e.g., 'BTC', 'ETH', 'SOL')"),
				"oid":  intProp("Order ID (oid) - the unique order identifier returned when order was placed", nil),
			}, "coin", "oid"),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return cancelOrder(ctx, c, args)
			},
		),
		tool(
			"hyperliquid_cancel_all_orders",
			"Cancel all open orders for the user. Fetches all open orders and cancels them.",
			schema(map[string]any{
				"userAddress": strProp("User address (optional, defaults to configured account)"),
				"dex":         strPropDefault("Perp dex name (optional)", ""),
			}),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return cancelAllOrders(ctx, c, args)
			},
		),
		tool(
			"hyperliquid_modify_order",
			"Modify an existing order",
			schema(map[string]any{
				"oid":        intProp("Order ID to modify", nil),
				"coin":       strProp("Coin/asset name (e.g., 'BTC', 'ETH', 'SOL')"),
				"isBuy":      map[string]any{"type": "boolean", "description": "True for buy orders, false for sell orders"},
				"size":       strProp("New order size"),
				"price":      strProp("New limit price"),
				"reduceOnly": boolPropDefault("Whether this is a reduce-only order", false),
				"orderType":  objPropDefault("Order type configuration", map[string]any{"limit": map[string]any{"tif": "Gtc"}}),
			}, "oid", "coin", "isBuy", "size", "price"),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return modifyOrder(ctx, c, args)
			},
		),
	}
}

// twapTools are the TWAP stubs: listed for schema parity but always fail
// (the Python reference raises NotImplementedError with these messages).
func twapTools() []server.ServerTool {
	return []server.ServerTool{
		tool(
			"hyperliquid_place_twap_order",
			"Place a Time-Weighted Average Price (TWAP) order",
			schema(map[string]any{
				"coin":       strProp("Coin/asset name (e.g., 'BTC', 'ETH', 'SOL')"),
				"isBuy":      map[string]any{"type": "boolean", "description": "True for buy orders, false for sell orders"},
				"size":       strProp("Total order size to be executed over time"),
				"minutes":    intProp("Duration in minutes for TWAP execution", int64ptr(2)),
				"reduceOnly": boolPropDefault("Whether this is a reduce-only order", false),
				"randomize":  boolPropDefault("Whether to randomize TWAP intervals", true),
			}, "coin", "isBuy", "size", "minutes"),
			func(context.Context, map[string]any) (map[string]any, error) {
				return nil, fmt.Errorf("TWAP orders require additional implementation")
			},
		),
		tool(
			"hyperliquid_cancel_twap_order",
			"Cancel a TWAP order by its ID",
			schema(map[string]any{
				"twapId": intProp("TWAP order ID to cancel", nil),
			}, "twapId"),
			func(context.Context, map[string]any) (map[string]any, error) {
				return nil, fmt.Errorf("TWAP cancellation requires additional implementation")
			},
		),
	}
}

// requireBoolArg reads a required boolean parameter (isBuy).
func requireBoolArg(args map[string]any, name string) (bool, error) {
	b, ok := args[name].(bool)
	if !ok {
		return false, fmt.Errorf("missing required parameter: %s", name)
	}
	return b, nil
}

// orderTypeArg converts an orderType-style argument to the SDK type,
// mirroring the Python handler: the default is {"limit": {"tif": "Gtc"}},
// and a string triggerPx is coerced to float by mutating the argument map in
// place, so the requestParams echo carries the coerced value.
func orderTypeArg(args map[string]any, name string) (hyperliquid.OrderType, error) {
	v := args[name]
	if v == nil {
		v = map[string]any{"limit": map[string]any{"tif": "Gtc"}}
	}
	m, ok := v.(map[string]any)
	if !ok {
		return hyperliquid.OrderType{}, fmt.Errorf("invalid %s parameter: must be an object", name)
	}
	if trigger, ok := m["trigger"].(map[string]any); ok {
		if s, ok := trigger["triggerPx"].(string); ok {
			f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil {
				return hyperliquid.OrderType{}, fmt.Errorf("invalid triggerPx parameter: %s. Must be a valid number.", s)
			}
			trigger["triggerPx"] = f
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return hyperliquid.OrderType{}, fmt.Errorf("invalid %s parameter: %v", name, err)
	}
	var ot hyperliquid.OrderType
	if err := json.Unmarshal(b, &ot); err != nil {
		return hyperliquid.OrderType{}, fmt.Errorf("invalid %s parameter: %v", name, err)
	}
	if ot.Limit == nil && ot.Trigger == nil {
		return hyperliquid.OrderType{}, fmt.Errorf(`invalid orderType: must contain "limit" or "trigger"`)
	}
	return ot, nil
}

// exchangeErr guards an SDK call result: a nil response means a transport/HTTP
// failure and becomes a tool error; a non-nil response is used even when the
// SDK promoted an API-level error into a Go error (Python post() only raises
// on HTTP >= 400).
func exchangeErr(err error) error {
	if err == nil {
		return fmt.Errorf("exchange returned no response")
	}
	return err
}

// mixedToAny converts the SDK's MixedArray (cancel statuses) into the generic
// slice the rebuilt envelope data needs, preserving the API's own values.
func mixedToAny(statuses hyperliquid.MixedArray) []any {
	if len(statuses) == 0 {
		return []any{}
	}
	b, err := json.Marshal(statuses)
	if err != nil {
		return []any{}
	}
	var out []any
	if err := json.Unmarshal(b, &out); err != nil {
		return []any{}
	}
	return out
}

func placeOrder(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	asset, err := RequireInt(args, "asset")
	if err != nil {
		return nil, err
	}
	isBuy, err := requireBoolArg(args, "isBuy")
	if err != nil {
		return nil, err
	}
	size, err := FloatParam(args, "size")
	if err != nil {
		return nil, err
	}
	price, err := FloatParamDefault(args, "price", 0)
	if err != nil {
		return nil, err
	}
	reduceOnly := OptBool(args, "reduceOnly", false)
	cloid := OptString(args, "cloid", "")
	var cloidPtr *string
	if cloid != "" {
		cloidPtr = &cloid
	}
	coin, err := c.CoinForAsset(ctx, asset)
	if err != nil {
		return nil, err
	}
	orderType, err := orderTypeArg(args, "orderType")
	if err != nil {
		return nil, err
	}

	resp, err := c.Exchange.BulkOrders(ctx, []hyperliquid.CreateOrderRequest{{
		Coin:          coin,
		IsBuy:         isBuy,
		Price:         price,
		Size:          size,
		ReduceOnly:    reduceOnly,
		OrderType:     orderType,
		ClientOrderID: cloidPtr,
	}}, nil)
	if resp == nil {
		return nil, exchangeErr(err)
	}
	if !resp.Ok {
		// Top-level {"status":"err","response":"reason"}: Python's
		// _parse_order_response raises on the string "response" value
		// (AttributeError → error envelope). Mirror with the API's reason.
		return nil, fmt.Errorf("order rejected: %s", resp.Err)
	}

	data := ExchangeDataMap(resp.Status, resp.Type, OrderStatusesToMaps(resp.Data.Statuses))
	orderInfo, err := ParseOrderResponse(data)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"message":       fmt.Sprintf("Order placed for %s", coin),
		"data":          data,
		"orderInfo":     orderInfo,
		"requestParams": args,
	}, nil
}

func placeBracketOrder(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	asset, err := RequireInt(args, "asset")
	if err != nil {
		return nil, err
	}
	isBuy, err := requireBoolArg(args, "isBuy")
	if err != nil {
		return nil, err
	}
	size, err := FloatParam(args, "size")
	if err != nil {
		return nil, err
	}
	entryPrice, err := FloatParamDefault(args, "entryPrice", 0)
	if err != nil {
		return nil, err
	}
	tpPrice, err := FloatParam(args, "takeProfitPrice")
	if err != nil {
		return nil, err
	}
	slPrice, err := FloatParam(args, "stopLossPrice")
	if err != nil {
		return nil, err
	}
	reduceOnly := OptBool(args, "reduceOnly", false)
	coin, err := c.CoinForAsset(ctx, asset)
	if err != nil {
		return nil, err
	}
	entryOrderType, err := orderTypeArg(args, "entryOrderType")
	if err != nil {
		return nil, err
	}

	orders := bracketOrders(bracketParams{
		coin:           coin,
		isBuy:          isBuy,
		size:           size,
		entryPrice:     entryPrice,
		tpPrice:        tpPrice,
		slPrice:        slPrice,
		reduceOnly:     reduceOnly,
		entryOrderType: entryOrderType,
	})

	resp, err := c.Exchange.BulkOrders(ctx, orders, nil)
	if resp == nil {
		return nil, exchangeErr(err)
	}
	if !resp.Ok {
		// Same top-level rejection behavior as placeOrder.
		return nil, fmt.Errorf("order rejected: %s", resp.Err)
	}

	statuses := resp.Data.Statuses
	if len(statuses) > 3 {
		return nil, fmt.Errorf("unexpected number of order statuses: %d", len(statuses))
	}
	data := ExchangeDataMap(resp.Status, resp.Type, OrderStatusesToMaps(statuses))
	labels := []string{"entry", "take-profit", "stop-loss"}
	orderInfos := make([]any, 0, len(statuses))
	for i, s := range statuses {
		info := ParseOrderStatus(OrderStatusToMap(s))
		info["orderType"] = labels[i]
		orderInfos = append(orderInfos, info)
	}
	return map[string]any{
		"message":       "Bracket order placed successfully",
		"data":          data,
		"orders":        orderInfos,
		"requestParams": args,
	}, nil
}

// bracketParams carries the inputs for building a bracket order triple.
type bracketParams struct {
	coin           string
	isBuy          bool
	size           float64
	entryPrice     float64
	tpPrice        float64
	slPrice        float64
	reduceOnly     bool
	entryOrderType hyperliquid.OrderType
}

// bracketOrders builds the entry + take-profit + stop-loss triple (server.py
// bracket construction): TP/SL are opposite-side, reduce-only trigger orders.
func bracketOrders(p bracketParams) []hyperliquid.CreateOrderRequest {
	trigger := func(px float64, tpsl hyperliquid.Tpsl) hyperliquid.OrderType {
		return hyperliquid.OrderType{Trigger: &hyperliquid.TriggerOrderType{
			TriggerPx: px,
			IsMarket:  false,
			Tpsl:      tpsl,
		}}
	}
	return []hyperliquid.CreateOrderRequest{
		{
			Coin:       p.coin,
			IsBuy:      p.isBuy,
			Price:      p.entryPrice,
			Size:       p.size,
			ReduceOnly: p.reduceOnly,
			OrderType:  p.entryOrderType,
		},
		{
			Coin:       p.coin,
			IsBuy:      !p.isBuy,
			Price:      p.tpPrice,
			Size:       p.size,
			ReduceOnly: true,
			OrderType:  trigger(p.tpPrice, hyperliquid.TakeProfit),
		},
		{
			Coin:       p.coin,
			IsBuy:      !p.isBuy,
			Price:      p.slPrice,
			Size:       p.size,
			ReduceOnly: true,
			OrderType:  trigger(p.slPrice, hyperliquid.StopLoss),
		},
	}
}

func cancelOrder(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	coin, err := RequireString(args, "coin")
	if err != nil {
		return nil, err
	}
	oid, err := RequireInt(args, "oid")
	if err != nil {
		return nil, err
	}

	resp, err := c.Exchange.Cancel(ctx, coin, oid)
	if resp == nil {
		return nil, exchangeErr(err)
	}

	// Python passes a top-level {"status":"err","response":"reason"} body
	// through as successful data (its cancel handler never parses statuses).
	data := map[string]any{}
	if !resp.Ok {
		data["status"] = resp.Status
		data["response"] = resp.Err
	} else {
		data = ExchangeDataMap(resp.Status, resp.Type, mixedToAny(resp.Data.Statuses))
	}

	return map[string]any{
		"message": fmt.Sprintf("Order %d cancelled for %s", oid, coin),
		"data":    data,
		"cancelledOrder": map[string]any{
			"coin":    coin,
			"orderId": oid,
		},
	}, nil
}

func cancelAllOrders(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
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
		// A literal null/empty body means no open orders, like Python's
		// `if not open_orders` over a None result.
		if s := strings.TrimSpace(string(raw)); s == "" || s == "null" {
			orders = nil
		} else {
			return nil, err
		}
	}

	if len(orders) == 0 {
		// Python returns this synthesized body when there is nothing to
		// cancel (no "type" key inside response).
		return map[string]any{
			"message": "No open orders to cancel",
			"data": map[string]any{
				"status": "ok",
				"response": map[string]any{
					"data": map[string]any{"statuses": []any{}},
				},
			},
			"cancelledCount": 0,
		}, nil
	}

	reqs := make([]hyperliquid.CancelOrderRequest, 0, len(orders))
	for _, o := range orders {
		order, ok := o.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("unexpected open order entry: %v", o)
		}
		coin, ok := order["coin"].(string)
		if !ok {
			return nil, fmt.Errorf("open order missing coin: %v", order)
		}
		oid, err := coerceInt64(order["oid"])
		if err != nil {
			return nil, fmt.Errorf("open order missing oid: %v", order)
		}
		reqs = append(reqs, hyperliquid.CancelOrderRequest{Coin: coin, OrderID: oid})
	}

	resp, err := c.Exchange.BulkCancel(ctx, reqs)
	if resp == nil {
		return nil, exchangeErr(err)
	}

	// Same top-level passthrough as cancelOrder.
	data := map[string]any{}
	if !resp.Ok {
		data["status"] = resp.Status
		data["response"] = resp.Err
	} else {
		data = ExchangeDataMap(resp.Status, resp.Type, mixedToAny(resp.Data.Statuses))
	}

	return map[string]any{
		"message":        fmt.Sprintf("Cancelled %d orders", len(reqs)),
		"data":           data,
		"cancelledCount": len(reqs),
	}, nil
}

func modifyOrder(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	oid, err := RequireInt(args, "oid")
	if err != nil {
		return nil, err
	}
	coin, err := RequireString(args, "coin")
	if err != nil {
		return nil, err
	}
	isBuy, err := requireBoolArg(args, "isBuy")
	if err != nil {
		return nil, err
	}
	size, err := FloatParam(args, "size")
	if err != nil {
		return nil, err
	}
	price, err := FloatParam(args, "price")
	if err != nil {
		return nil, err
	}
	reduceOnly := OptBool(args, "reduceOnly", false)
	orderType, err := orderTypeArg(args, "orderType")
	if err != nil {
		return nil, err
	}

	// The Python SDK's modify_order delegates to bulk_modify_orders_new, so
	// the wire action is "batchModify"; BulkModifyOrders reproduces it.
	statuses, err := c.Exchange.BulkModifyOrders(ctx, []hyperliquid.ModifyOrderRequest{{
		Oid: &oid,
		Order: hyperliquid.CreateOrderRequest{
			Coin:       coin,
			IsBuy:      isBuy,
			Price:      price,
			Size:       size,
			ReduceOnly: reduceOnly,
			OrderType:  orderType,
		},
	}})
	if err != nil {
		// Modify exception: the SDK hides a non-ok body behind a Go error,
		// so an API-level rejection surfaces as a tool error here.
		return nil, err
	}

	data := ExchangeDataMap("ok", "batchModify", OrderStatusesToMaps(statuses))
	return map[string]any{
		"message": fmt.Sprintf("Order %d modified successfully", oid),
		"data":    data,
		"modifiedOrder": map[string]any{
			"orderId":  oid,
			"coin":     coin,
			"newPrice": price,
			"newSize":  size,
		},
	}, nil
}
