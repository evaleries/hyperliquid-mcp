package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/bytedance/sonic"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/sonirico/go-hyperliquid"
)

// envelopeJSON is the envelope render/decode engine: sonic, configured for
// byte-parity with encoding/json as used by marshalPretty (sorted map keys,
// no HTML escaping, compact RawMessage marshaling, copied strings). Verified
// byte-identical to the previous stdlib encoder on every envelope shape in
// the test suite; the one known divergence is U+2028/U+2029, which sonic
// emits raw (valid JSON; encoding/json escapes them even with HTML escaping
// off). Hyperliquid payloads are ASCII, so the case does not occur in
// practice; both forms decode identically.
//
// encoding/json remains in use for rawArrayLen (sonic has no streaming Token
// decoder) and for one-shot schema building at startup.
var envelopeJSON = sonic.Config{
	EscapeHTML:       false,
	SortMapKeys:      true,
	CompactMarshaler: true,
	CopyString:       true,
}.Froze()

// marshalPretty renders the response envelope like Python's
// json.dumps(indent=2): 2-space indent, no HTML escaping, no trailing
// newline.
func marshalPretty(v any) (string, error) {
	// sonic.MarshalIndent emits no trailing newline, matching the trimmed
	// stdlib encoder output byte-for-byte (see envelopeJSON comment).
	b, err := envelopeJSON.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// successResult wraps a successful payload as a single text content item.
func successResult(v any) (*mcp.CallToolResult, error) {
	text, err := marshalPretty(v)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(text), nil
}

// errorResult builds the Python-parity error envelope
// {"error", "tool", "arguments"} and marks the MCP result isError: true
// (documented improvement over Python).
func errorResult(tool string, args map[string]any, err error) (*mcp.CallToolResult, error) {
	envelope := map[string]any{
		"error":     err.Error(),
		"tool":      tool,
		"arguments": args,
	}
	text, mErr := marshalPretty(envelope)
	if mErr != nil {
		return nil, mErr
	}
	return mcp.NewToolResultError(text), nil
}

// handlerFunc is the internal shape of a tool handler: it receives the
// (already int-normalized) arguments and returns the success envelope map.
type handlerFunc func(ctx context.Context, args map[string]any) (map[string]any, error)

// wrap adapts a handlerFunc to mcp-go, reproducing the Python call_tool
// behavior: integer parameters are normalized before dispatch, and every
// error becomes the error envelope (never a Go error return).
func wrap(name string, h handlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		if args == nil {
			args = map[string]any{}
		}
		if err := NormalizeIntParams(args); err != nil {
			return errorResult(name, args, err)
		}
		res, err := h(ctx, args)
		if err != nil {
			return errorResult(name, args, err)
		}
		return successResult(res)
	}
}

// ParseOrderStatus converts one entry of response.data.statuses into the
// parsed contract shape (Python _parse_order_status).
// Field names here are part of the LLM-facing contract.
func ParseOrderStatus(status map[string]any) map[string]any {
	if resting, ok := status["resting"].(map[string]any); ok {
		return map[string]any{
			"status":  "resting",
			"orderId": resting["oid"],
			"message": "Order placed and resting on order book",
		}
	}
	if filled, ok := status["filled"].(map[string]any); ok {
		return map[string]any{
			"status":       "filled",
			"orderId":      filled["oid"],
			"totalSize":    filled["totalSz"],
			"averagePrice": filled["avgPx"],
			"message":      "Order filled successfully",
		}
	}
	if errMsg, ok := status["error"]; ok {
		return map[string]any{
			"status":  "error",
			"error":   errMsg,
			"message": "Order placement failed",
		}
	}
	return map[string]any{
		"status":    "unknown",
		"rawStatus": status,
	}
}

// ParseOrderResponse parses statuses[0] of an order placement response
// (Python _parse_order_response: `result.get("response", {}).get("data",
// {}).get("statuses", [{}])[0]`). A missing statuses key yields the "unknown"
// shape (Python's default); a present-but-empty list is an error (Python
// IndexError → error envelope).
func ParseOrderResponse(data map[string]any) (map[string]any, error) {
	response, _ := data["response"].(map[string]any)
	respData, _ := response["data"].(map[string]any)
	raw, present := respData["statuses"].([]any)
	if !present {
		return ParseOrderStatus(map[string]any{}), nil
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("no order status in response")
	}
	first, ok := raw[0].(map[string]any)
	if !ok {
		return ParseOrderStatus(map[string]any{}), nil
	}
	return ParseOrderStatus(first), nil
}

// OrderStatusToMap reconstructs the API wire shape of one order status from
// the SDK's typed value, so the envelope's data field matches what the
// Python server passes through.
func OrderStatusToMap(s hyperliquid.OrderStatus) map[string]any {
	switch {
	case s.Resting != nil:
		resting := map[string]any{"oid": s.Resting.Oid}
		if s.Resting.ClientID != nil {
			resting["cloid"] = *s.Resting.ClientID
		}
		if s.Resting.Status != "" {
			resting["status"] = s.Resting.Status
		}
		return map[string]any{"resting": resting}
	case s.Filled != nil:
		return map[string]any{"filled": map[string]any{
			"totalSz": s.Filled.TotalSz,
			"avgPx":   s.Filled.AvgPx,
			"oid":     s.Filled.Oid,
		}}
	case s.Error != nil:
		return map[string]any{"error": *s.Error}
	default:
		return map[string]any{}
	}
}

// OrderStatusesToMaps converts a full statuses slice.
func OrderStatusesToMaps(statuses []hyperliquid.OrderStatus) []any {
	out := make([]any, len(statuses))
	for i, s := range statuses {
		out[i] = OrderStatusToMap(s)
	}
	return out
}

// ExchangeDataMap rebuilds the Python-passed-through exchange response shape
// {"status","response":{"type","data":{"statuses"}}} from the SDK's parsed
// APIResponse fields.
func ExchangeDataMap(status, respType string, statuses []any) map[string]any {
	return map[string]any{
		"status": status,
		"response": map[string]any{
			"type": respType,
			"data": map[string]any{"statuses": statuses},
		},
	}
}

// rawToMap decodes a raw /info response body for summary computation. The
// envelope keeps the original json.RawMessage untouched; this decoded form is
// only for reading fields, mirroring Python's dict indexing.
func rawToMap(raw json.RawMessage) (map[string]any, error) {
	var m map[string]any
	if err := envelopeJSON.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}
	return m, nil
}

// rawToSlice decodes a raw /info response body that is a JSON array.
func rawToSlice(raw json.RawMessage) ([]any, error) {
	var s []any
	if err := envelopeJSON.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}
	return s, nil
}

// rawArrayLen counts the top-level elements of a JSON array body in O(1)
// memory. rawToSlice materializes every element as map/slice garbage —
// measured 77MB allocated for a 2.7MB candleSnapshot body (29×) — while the
// large-array handlers (candles, user fills, user/historical funding) only
// need len() for the summary. Each element is still fully decoded (into a
// discarded RawMessage), so malformed JSON fails exactly like rawToSlice.
// A JSON null body yields 0, mirroring rawToSlice's nil-slice mapping.
func rawArrayLen(raw json.RawMessage) (int, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return 0, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return 0, fmt.Errorf("failed to parse API response: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '[' {
		return 0, fmt.Errorf("failed to parse API response: expected JSON array, got %v", tok)
	}
	// discard is hoisted: RawMessage.UnmarshalJSON resets and reuses the
	// backing array, so the loop allocates once (largest element) instead of
	// once per element. The value is never retained.
	n := 0
	var discard json.RawMessage
	for dec.More() {
		if err := dec.Decode(&discard); err != nil {
			return 0, fmt.Errorf("failed to parse API response: %w", err)
		}
		n++
	}
	if _, err := dec.Token(); err != nil { // closing ']'
		return 0, fmt.Errorf("failed to parse API response: %w", err)
	}
	if tok, err := dec.Token(); err != io.EOF { // trailing data after ']'
		if err != nil {
			return 0, fmt.Errorf("failed to parse API response: %w", err)
		}
		return 0, fmt.Errorf("failed to parse API response: unexpected data after array: %v", tok)
	}
	return n, nil
}

// dataMap is a small typed accessor over decoded API responses, mirroring
// Python's result["path"]["to"] chains. ok is false when any level is absent
// or has an unexpected type.
func digMap(m map[string]any, keys ...string) (map[string]any, bool) {
	cur := m
	for i, k := range keys {
		v, ok := cur[k]
		if !ok {
			return nil, false
		}
		if i == len(keys)-1 {
			out, ok := v.(map[string]any)
			return out, ok
		}
		cur, ok = v.(map[string]any)
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// digString reads a string field, tolerating numeric rendering
// ("" when absent).
func digString(m map[string]any, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

// digLen returns the length of the array at key, or 0 (Python
// len(result["x"]) with a guard).
func digLen(m map[string]any, key string) int {
	if arr, ok := m[key].([]any); ok {
		return len(arr)
	}
	return 0
}
