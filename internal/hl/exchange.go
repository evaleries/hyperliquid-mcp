// Raw signed /exchange support for actions the SDK does not model yet.
//
// The trailingStop action exists only as a wire format reverse-engineered
// from the Hyperliquid frontend (docs/DECISIONS.md D-15); go-hyperliquid
// v0.44.2 has no typed builder for it. The Python reference faces the same
// gap and solves it with the SDK's exported helpers (sign_l1_action +
// Exchange._post_action). This file mirrors that exactly: signing stays
// inside the SDK (the exported hyperliquid.SignL1Action); only payload
// assembly and the POST are reimplemented, following RawInfo's posture.

package hl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/sonirico/go-hyperliquid"
)

// exchangeActionPayload mirrors hyperliquid-python-sdk Exchange._post_action:
// the vaultAddress and expiresAfter keys are always present (null when
// unset). Key order of the outer payload is irrelevant to the action hash —
// only the action object itself is msgpack-serialized for signing.
type exchangeActionPayload struct {
	Action       any                         `json:"action"`
	Nonce        int64                       `json:"nonce"`
	Signature    hyperliquid.SignatureResult `json:"signature"`
	VaultAddress *string                     `json:"vaultAddress"`
	ExpiresAfter *int64                      `json:"expiresAfter"`
}

// RawExchange L1-signs an arbitrary exchange action and POSTs it to
// {baseURL}/exchange, returning the response body untouched — the /exchange
// counterpart of RawInfo. The nonce comes from the process-wide floor
// (nextNonceSeed), like the builder-dex exchanges; the SDK exchange's own
// counter stays independent (same accepted collision profile as D-14).
// expiresAfter is never set, matching the Python server (its
// exchange.expires_after is always None).
func (c *Client) RawExchange(ctx context.Context, action any) (json.RawMessage, error) {
	nonce := c.nextNonceSeed()
	sig, err := hyperliquid.SignL1Action(
		c.key, action, c.VaultAddress, nonce, nil, c.BaseURL == hyperliquid.MainnetAPIURL,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to sign exchange action: %w", err)
	}

	payload := exchangeActionPayload{
		Action:    action,
		Nonce:     nonce,
		Signature: sig,
	}
	if c.VaultAddress != "" {
		payload.VaultAddress = &c.VaultAddress
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode exchange request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/exchange", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchange request failed: %w", err)
	}
	return readAPIResponse(resp, "exchange")
}

// FloatToWire renders a float64 in Hyperliquid's wire format, mirroring
// hyperliquid-python-sdk float_to_wire (and the Go SDK's unexported
// floatToWire): 8-decimal fixed rounding, an error when that rounding loses
// information, -0 normalized to 0, trailing zeros stripped.
func FloatToWire(x float64) (string, error) {
	rounded := strconv.FormatFloat(x, 'f', 8, 64)
	parsed, err := strconv.ParseFloat(rounded, 64)
	if err != nil {
		return "", err
	}
	if math.Abs(parsed-x) >= 1e-12 {
		return "", fmt.Errorf("float_to_wire causes rounding: %v", x)
	}
	if rounded == "-0.00000000" {
		rounded = "0.00000000"
	}
	result := strings.TrimRight(rounded, "0")
	result = strings.TrimRight(result, ".")
	return result, nil
}
