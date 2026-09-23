package tools

// Trailing stop (post-parity extension).
//
// Hyperliquid's trailing stop is a standalone "trailingStop" exchange action
// — not an orderType inside the regular "order" action — reverse-engineered
// from the Hyperliquid frontend (no API docs or upstream SDK coverage yet).
// All wire-format and signing code lives in the SDK
// (Exchange.PlaceTrailingStop, currently via the evaleries/go-hyperliquid
// fork — see go.mod replace and docs/DECISIONS.md D-15); this file only
// validates arguments with the reference's messages and rebuilds the
// envelope. Cancellation works through the existing cancel action, so
// cancel_order / cancel_all_orders need no changes.

import (
	"context"
	"fmt"
	"strings"

	"github.com/sonirico/go-hyperliquid"

	"github.com/evaleries/hyperliquid-mcp/internal/hl"
)

func placeTrailingStopOrder(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	asset, err := RequireInt(args, "asset")
	if err != nil {
		return nil, err
	}
	isBuy, err := requireBoolArg(args, "isBuy")
	if err != nil {
		return nil, err
	}
	req, err := trailingStopRequest(args, isBuy)
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
	req.Coin = coin

	resp, err := c.Exchange.PlaceTrailingStop(ctx, *req)
	if resp == nil {
		return nil, exchangeErr(err)
	}
	if !resp.Ok {
		// Same top-level rejection behavior as placeOrder.
		return nil, fmt.Errorf("order rejected: %s", resp.Err)
	}
	data := ExchangeDataMap(resp.Status, resp.Type, OrderStatusesToMaps(resp.Data.Statuses))
	orderInfo, err := ParseOrderResponse(data)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"message":       fmt.Sprintf("Trailing stop order placed for %s", coin),
		"data":          data,
		"orderInfo":     orderInfo,
		"requestParams": args,
	}, nil
}

// trailingStopRequest validates the tool arguments into the SDK request,
// mirroring the reference's _build_trailing_stop_action validation order and
// messages. Wire formatting (floatToWire, "1.5000%" rendering) happens
// inside the SDK.
func trailingStopRequest(args map[string]any, isBuy bool) (*hyperliquid.TrailingStopOrderRequest, error) {
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

	retracement, err := FloatParam(args, "retracement")
	if err != nil {
		return nil, err
	}
	if retracement <= 0 {
		return nil, fmt.Errorf("Invalid retracement: %v. Must be positive.", args["retracement"])
	}
	var retr hyperliquid.TrailingStopRetracement
	if unit == "percent" {
		retr.Percent = &retracement
	} else {
		retr.PriceDistance = &retracement
	}

	// Absent, null, or blank activation price → nil (tracking starts
	// immediately), mirroring the reference's `str(x).strip() != ""` guard;
	// non-string numbers always apply and are accepted via the same coercion.
	var activationPx *float64
	if v, present := args["activationPrice"]; present && v != nil {
		s, isStr := v.(string)
		if blank := isStr && strings.TrimSpace(s) == ""; !blank {
			f, err := coerceFloat(v)
			if err != nil {
				return nil, fmt.Errorf("invalid activationPrice parameter: %v. Must be a valid number.", v)
			}
			activationPx = &f
		}
	}

	return &hyperliquid.TrailingStopOrderRequest{
		IsBuy:        isBuy,
		Size:         size,
		ReduceOnly:   OptBool(args, "reduceOnly", false),
		Retracement:  retr,
		ActivationPx: activationPx,
	}, nil
}
