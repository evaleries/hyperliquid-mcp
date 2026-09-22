package tools

// Trailing stop (post-parity extension).
//
// Hyperliquid's trailing stop is so new that neither the API docs nor any
// SDK covers it: the reference implementation (Python server.py working
// tree) reverse-engineered the wire format from the Hyperliquid frontend.
// It is a standalone "trailingStop" exchange action — not an orderType
// inside the regular "order" action — signed with standard L1 action
// signing. Cancellation works through the existing cancel action, so
// cancel_order / cancel_all_orders need no changes. See docs/DECISIONS.md
// D-15.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/evaleries/hyperliquid-mcp/internal/hl"
)

// trailingStopAction is the wire format of the trailingStop exchange action.
// Field declaration order is load-bearing: L1 signing msgpack-serializes
// fields in order and the API recomputes the hash from the posted JSON, so
// both must match the frontend's key order — the same order the Python
// reference builds its dict in.
type trailingStopAction struct {
	Type         string          `json:"type" msgpack:"type"`
	Asset        int64           `json:"asset" msgpack:"asset"`
	IsBuy        bool            `json:"isBuy" msgpack:"isBuy"`
	Sz           string          `json:"sz" msgpack:"sz"`
	ReduceOnly   bool            `json:"reduceOnly" msgpack:"reduceOnly"`
	Retracement  retracementWire `json:"retracement" msgpack:"retracement"`
	ActivationPx *string         `json:"activationPx" msgpack:"activationPx"`
}

// retracementWire is the trailing retracement spec: exactly one of Pct
// (percentage of the watermark price, e.g. "1.5000%") or Px (fixed price
// distance in quote currency, wire float string).
type retracementWire struct {
	Pct *string `json:"pct,omitempty" msgpack:"pct,omitempty"`
	Px  *string `json:"px,omitempty" msgpack:"px,omitempty"`
}

func placeTrailingStopOrder(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	asset, err := RequireInt(args, "asset")
	if err != nil {
		return nil, err
	}
	isBuy, err := requireBoolArg(args, "isBuy")
	if err != nil {
		return nil, err
	}
	action, err := buildTrailingStopAction(args, asset, isBuy)
	if err != nil {
		return nil, err
	}
	// Main-DEX meta lookup for the coin name (the reference calls
	// info.meta() per order; HIP-3 builder IDs are rejected here, as they
	// are by the reference's universe indexing).
	coin, err := c.CoinForAsset(ctx, asset)
	if err != nil {
		return nil, err
	}

	raw, err := c.RawExchange(ctx, action)
	if err != nil {
		return nil, err
	}
	parsed, err := rawToMap(raw)
	if err != nil {
		return nil, err
	}
	// Top-level {"status":"err","response":"reason"}: Python's
	// _parse_order_response raises on the string "response" value
	// (AttributeError → error envelope). Mirror with the API's reason, as
	// placeOrder does.
	if status, _ := parsed["status"].(string); status == "err" {
		return nil, fmt.Errorf("order rejected: %v", parsed["response"])
	}
	orderInfo, err := ParseOrderResponse(parsed)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"message":       fmt.Sprintf("Trailing stop order placed for %s", coin),
		"data":          json.RawMessage(raw),
		"orderInfo":     orderInfo,
		"requestParams": args,
	}, nil
}

// buildTrailingStopAction mirrors the reference's _build_trailing_stop_action:
// same validation messages, same wire shape.
func buildTrailingStopAction(args map[string]any, asset int64, isBuy bool) (*trailingStopAction, error) {
	unit := "percent"
	if v, present := args["retracementUnit"]; present {
		s, ok := v.(string)
		if !ok || (s != "percent" && s != "quote") {
			return nil, fmt.Errorf("Invalid retracementUnit: %v. Must be 'percent' or 'quote'.", v)
		}
		unit = s
	}

	size, err := FloatParam(args, "size")
	if err != nil {
		return nil, err
	}
	if size <= 0 {
		return nil, fmt.Errorf("Invalid size: %v. Must be positive.", args["size"])
	}
	sz, err := hl.FloatToWire(size)
	if err != nil {
		return nil, err
	}

	retracement, err := FloatParam(args, "retracement")
	if err != nil {
		return nil, err
	}
	if retracement <= 0 {
		return nil, fmt.Errorf("Invalid retracement: %v. Must be positive.", args["retracement"])
	}
	var retrWire retracementWire
	if unit == "percent" {
		// Percent retracement is sent as a string with 4 decimals and a '%'
		// suffix (e.g. "1.5000%"), like the reference's f"{v:.4f}%".
		pct := fmt.Sprintf("%.4f%%", retracement)
		retrWire.Pct = &pct
	} else {
		px, err := hl.FloatToWire(retracement)
		if err != nil {
			return nil, err
		}
		retrWire.Px = &px
	}

	// Absent, null, or blank activation price → null (tracking starts
	// immediately), mirroring the reference's `str(x).strip() != ""` guard;
	// non-string numbers always apply and are accepted via the same coercion.
	var activationPx *string
	if v, present := args["activationPrice"]; present && v != nil {
		s, isStr := v.(string)
		if blank := isStr && strings.TrimSpace(s) == ""; !blank {
			f, err := coerceFloat(v)
			if err != nil {
				return nil, fmt.Errorf("invalid activationPrice parameter: %v. Must be a valid number.", v)
			}
			w, err := hl.FloatToWire(f)
			if err != nil {
				return nil, err
			}
			activationPx = &w
		}
	}

	return &trailingStopAction{
		Type:         "trailingStop",
		Asset:        asset,
		IsBuy:        isBuy,
		Sz:           sz,
		ReduceOnly:   OptBool(args, "reduceOnly", false),
		Retracement:  retrWire,
		ActivationPx: activationPx,
	}, nil
}
