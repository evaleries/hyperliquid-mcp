package tools

import (
	"strings"
	"testing"
)

func TestNormalizeIntParams(t *testing.T) {
	tests := []struct {
		name    string
		in      map[string]any
		want    map[string]int64
		wantErr string
	}{
		{"float whole", map[string]any{"asset": 5.0}, map[string]int64{"asset": 5}, ""},
		{"float fraction truncates", map[string]any{"oid": 5.9}, map[string]int64{"oid": 5}, ""},
		{"float negative truncates toward zero", map[string]any{"oid": -2.7}, map[string]int64{"oid": -2}, ""},
		{"string number", map[string]any{"asset": "5"}, map[string]int64{"asset": 5}, ""},
		{"string float", map[string]any{"startTime": "1725000000000.0"}, map[string]int64{"startTime": 1725000000000}, ""},
		{"bool true", map[string]any{"minutes": true}, map[string]int64{"minutes": 1}, ""},
		{"bool false", map[string]any{"minutes": false}, map[string]int64{"minutes": 0}, ""},
		{"zero", map[string]any{"twapId": 0.0}, map[string]int64{"twapId": 0}, ""},
		{"non-numeric string", map[string]any{"asset": "abc"}, nil, "Invalid asset parameter: abc. Must be a valid integer."},
		{"map value", map[string]any{"oid": map[string]any{"a": 1.0}}, nil, "Invalid oid parameter:"},
		{"nan string", map[string]any{"asset": "nan"}, nil, "Invalid asset parameter: nan. Must be a valid integer."},
		{"inf string", map[string]any{"asset": "inf"}, nil, "Invalid asset parameter: inf. Must be a valid integer."},
		{"out of int64 range", map[string]any{"asset": 1e300}, nil, "Invalid asset parameter:"},
		{"negative out of range", map[string]any{"asset": -1e300}, nil, "Invalid asset parameter:"},
		{"2^63 boundary rejected", map[string]any{"oid": 9.223372036854776e18}, nil, "Invalid oid parameter:"},
		{"min int64 exact", map[string]any{"oid": -9223372036854775808.0}, map[string]int64{"oid": -9223372036854775808}, ""},
		{"large in-range", map[string]any{"startTime": 1e15}, map[string]int64{"startTime": 1000000000000000}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NormalizeIntParams(tt.in)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if strings.HasSuffix(tt.wantErr, ":") {
					if !strings.Contains(err.Error(), tt.wantErr) {
						t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantErr)
					}
				} else if err.Error() != tt.wantErr {
					t.Fatalf("error = %q, want exactly %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for k, want := range tt.want {
				got, ok := tt.in[k].(int64)
				if !ok {
					t.Fatalf("%s: type %T, want int64", k, tt.in[k])
				}
				if got != want {
					t.Errorf("%s = %d, want %d", k, got, want)
				}
			}
		})
	}
}

func TestNormalizeIntParamsSkipsAbsentAndNull(t *testing.T) {
	args := map[string]any{"asset": nil, "coin": "BTC"}
	if err := NormalizeIntParams(args); err != nil {
		t.Fatalf("null must be skipped: %v", err)
	}
	if args["asset"] != nil {
		t.Errorf("null asset should remain nil: %v", args["asset"])
	}
	if args["coin"] != "BTC" {
		t.Errorf("non-int param touched: %v", args["coin"])
	}
}

func TestPyFloatStr(t *testing.T) {
	// All expectations verified against Python 3.12 str(float).
	tests := []struct {
		in   float64
		want string
	}{
		{9950.0, "9950.0"},
		{0.5, "0.5"},
		{-2.25, "-2.25"},
		{0.0, "0.0"},
		{1e20, "1e+20"},
		{1000000.0, "1000000.0"},
		{1234567.0, "1234567.0"},
		{1500000.5, "1500000.5"},
		{9999999.0, "9999999.0"},
		{1e15, "1000000000000000.0"},
		{1e16, "1e+16"},
		{0.0001, "0.0001"},
		{0.00001, "1e-05"},
		{1.5e-5, "1.5e-05"},
		{123456789.123, "123456789.123"},
	}
	for _, tt := range tests {
		if got := PyFloatStr(tt.in); got != tt.want {
			t.Errorf("PyFloatStr(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFloatParam(t *testing.T) {
	args := map[string]any{"size": "0.1", "price": 181.5}
	sz, err := FloatParam(args, "size")
	if err != nil || sz != 0.1 {
		t.Errorf("size = %v, %v", sz, err)
	}
	px, err := FloatParam(args, "price")
	if err != nil || px != 181.5 {
		t.Errorf("price = %v, %v", px, err)
	}
	if _, err := FloatParam(args, "missing"); err == nil {
		t.Error("missing param must error")
	}
	if _, err := FloatParam(map[string]any{"size": "abc"}, "size"); err == nil {
		t.Error("non-numeric size must error")
	}
}

func TestFloatParamDefault(t *testing.T) {
	if f, _ := FloatParamDefault(map[string]any{}, "price", 0); f != 0 {
		t.Errorf("absent: %v", f)
	}
	if f, _ := FloatParamDefault(map[string]any{"price": ""}, "price", 0); f != 0 {
		t.Errorf("empty string: %v", f)
	}
	if f, _ := FloatParamDefault(map[string]any{"price": "181.5"}, "price", 0); f != 181.5 {
		t.Errorf("string: %v", f)
	}
	if f, _ := FloatParamDefault(map[string]any{"price": nil}, "price", 0); f != 0 {
		t.Errorf("null: %v", f)
	}
}
