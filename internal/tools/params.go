// Package tools implements the 23 MCP tools with parity to the Python
// reference server. Files mirror the section layout of server.py.
package tools

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/edkdev/hyperliquid-mcp-go/internal/hl"
)

// intParams are normalized to integers before dispatch, mirroring the Python
// server's int(float(x)) coercion.
var intParams = []string{"asset", "oid", "startTime", "endTime", "twapId", "minutes"}

// NormalizeIntParams coerces the integer-declared parameters in place, exactly
// like the Python `_handle_tool_call` pre-dispatch normalization. A present
// null is skipped (Python checks `is not None`). The error message matches the
// Python wording: `Invalid {param} parameter: {value}. Must be a valid integer.`
func NormalizeIntParams(args map[string]any) error {
	for _, p := range intParams {
		v, ok := args[p]
		if !ok || v == nil {
			continue
		}
		n, err := coerceInt64(v)
		if err != nil {
			return fmt.Errorf("Invalid %s parameter: %v. Must be a valid integer.", p, v)
		}
		args[p] = n
	}
	return nil
}

// coerceInt64 implements Python int(float(x)) semantics: truncation toward
// zero; strings parsed via float; booleans as 1/0.
func coerceInt64(v any) (int64, error) {
	switch t := v.(type) {
	case float64:
		return truncInt64(t)
	case float32:
		return truncInt64(float64(t))
	case int:
		return int64(t), nil
	case int64:
		return t, nil
	case int32:
		return int64(t), nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil {
			return 0, err
		}
		return truncInt64(f)
	case bool:
		if t {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("not numeric: %T", v)
	}
}

func truncInt64(f float64) (int64, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("not finite: %v", f)
	}
	// Go's float64→int64 conversion is implementation-defined out of range
	// (saturates on arm64); Python's int(float(x)) is exact. Error instead of
	// silently rewriting the value (security review SEC-NUMERIC-003).
	// Note float64(math.MaxInt64) == 2^63 (MaxInt64 itself is not
	// representable), so the guard uses >= against 2^63: any float64 that
	// rounds to 2^63 is out of int64 range.
	if f >= float64(math.MaxInt64) || f < float64(math.MinInt64) {
		return 0, fmt.Errorf("out of int64 range: %v", f)
	}
	return int64(f), nil
}

// RequireInt reads a normalized integer parameter.
func RequireInt(args map[string]any, name string) (int64, error) {
	v, ok := args[name]
	if !ok || v == nil {
		return 0, fmt.Errorf("missing required parameter: %s", name)
	}
	n, err := coerceInt64(v)
	if err != nil {
		return 0, fmt.Errorf("Invalid %s parameter: %v. Must be a valid integer.", name, v)
	}
	return n, nil
}

// RequireString reads a required string parameter.
func RequireString(args map[string]any, name string) (string, error) {
	v, ok := args[name]
	if !ok || v == nil {
		return "", fmt.Errorf("missing required parameter: %s", name)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("parameter %s must be a string, got %v", name, v)
	}
	return s, nil
}

// OptString reads an optional string parameter.
func OptString(args map[string]any, name, def string) string {
	if s, ok := args[name].(string); ok {
		return s
	}
	return def
}

// OptBool reads an optional boolean parameter.
func OptBool(args map[string]any, name string, def bool) bool {
	if b, ok := args[name].(bool); ok {
		return b
	}
	return def
}

// FloatParam reads a required float parameter given as a JSON string or
// number (Python float(x) semantics).
func FloatParam(args map[string]any, name string) (float64, error) {
	v, ok := args[name]
	if !ok || v == nil {
		return 0, fmt.Errorf("missing required parameter: %s", name)
	}
	f, err := coerceFloat(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s parameter: %v. Must be a valid number.", name, v)
	}
	return f, nil
}

// FloatParamDefault reads an optional float parameter. Absent, null, or empty
// string yield the default (mirrors Python `float(s) if s else default`).
func FloatParamDefault(args map[string]any, name string, def float64) (float64, error) {
	v, ok := args[name]
	if !ok || v == nil {
		return def, nil
	}
	if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
		return def, nil
	}
	f, err := coerceFloat(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s parameter: %v. Must be a valid number.", name, v)
	}
	return f, nil
}

func coerceFloat(v any) (float64, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case float32:
		return float64(t), nil
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(t), 64)
	default:
		return 0, fmt.Errorf("not numeric: %T", v)
	}
}

// UserAddress resolves userAddress with the configured account as default
// (parity with the Python server).
func UserAddress(args map[string]any, c *hl.Client) string {
	if s, ok := args["userAddress"].(string); ok && s != "" {
		return s
	}
	return c.AccountAddress
}

// Dex resolves the dex parameter (always defaults to "", parity rule 5).
func Dex(args map[string]any) string {
	return OptString(args, "dex", "")
}

// PyFloatStr renders a float64 the way CPython's str(float) does: shortest
// round-trip decimal, fixed notation for 1e-4 <= |x| < 1e16, scientific
// notation outside that range, always with a fractional part for integral
// values ("9950.0", not "9950"; "1234567.0", not "1.234567e+06").
// Verified against Python 3.12 on a boundary sweep (review finding 2).
func PyFloatStr(f float64) string {
	if math.IsNaN(f) {
		return "nan"
	}
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	var s string
	if abs := math.Abs(f); f == 0 || (abs >= 1e-4 && abs < 1e16) {
		s = strconv.FormatFloat(f, 'f', -1, 64)
	} else {
		s = strconv.FormatFloat(f, 'e', -1, 64)
	}
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}
