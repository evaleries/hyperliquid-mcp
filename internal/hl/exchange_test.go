package hl

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

// testAction is a minimal ordered action for RawExchange wire assertions.
type testAction struct {
	Type  string `json:"type" msgpack:"type"`
	Asset int64  `json:"asset" msgpack:"asset"`
	Note  string `json:"note" msgpack:"note"`
}

// exchangeSpy serves the startup /info fetches and records the raw body of
// the next /exchange POST.
type exchangeSpy struct {
	server   *httptest.Server
	rawBody  string
	status   int
	response string
}

func newExchangeSpy(t *testing.T) *exchangeSpy {
	t.Helper()
	spy := &exchangeSpy{status: http.StatusOK, response: `{"status":"ok","response":{"type":"test","data":{}}}`}
	spy.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/info" {
			var req struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			switch req.Type {
			case "meta":
				_, _ = w.Write([]byte(metaFixture))
			case "spotMeta":
				_, _ = w.Write([]byte(spotFixture))
			default:
				http.Error(w, "unexpected info type: "+req.Type, http.StatusBadRequest)
			}
			return
		}
		spy.rawBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(spy.status)
		_, _ = w.Write([]byte(spy.response))
	}))
	t.Cleanup(spy.server.Close)
	return spy
}

func TestRawExchangePostShape(t *testing.T) {
	spy := newExchangeSpy(t)
	c, err := New(context.Background(), testConfig(t, spy.server.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	raw, err := c.RawExchange(context.Background(), testAction{Type: "testAction", Asset: 5, Note: "hi"})
	if err != nil {
		t.Fatalf("RawExchange: %v", err)
	}
	if string(raw) != spy.response {
		t.Errorf("response body not passed through: %s", raw)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(spy.rawBody), &payload); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	if _, ok := payload["nonce"].(float64); !ok {
		t.Errorf("nonce missing or not a number: %v", payload["nonce"])
	}
	sig, ok := payload["signature"].(map[string]any)
	if !ok {
		t.Fatalf("signature missing: %v", payload)
	}
	for _, k := range []string{"r", "s", "v"} {
		if _, ok := sig[k]; !ok {
			t.Errorf("signature missing %q: %v", k, sig)
		}
	}
	// Python _post_action parity: vaultAddress and expiresAfter keys are
	// always present, null when unset.
	if v, ok := payload["vaultAddress"]; !ok || v != nil {
		t.Errorf("vaultAddress = %v (present: %v), want present null", v, ok)
	}
	if v, ok := payload["expiresAfter"]; !ok || v != nil {
		t.Errorf("expiresAfter = %v (present: %v), want present null", v, ok)
	}
	// Struct field order must survive into the JSON body (the API recomputes
	// the action hash from the posted key order).
	if !strings.Contains(spy.rawBody, `"action":{"type":"testAction","asset":5,"note":"hi"}`) {
		t.Errorf("action key order lost in body: %s", spy.rawBody)
	}
}

func TestRawExchangeVaultAddress(t *testing.T) {
	spy := newExchangeSpy(t)
	cfg := testConfig(t, spy.server.URL)
	cfg.VaultAddress = "0x00000000000000000000000000000000deadbeef"
	c, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := c.RawExchange(context.Background(), testAction{Type: "testAction", Asset: 1, Note: "v"}); err != nil {
		t.Fatalf("RawExchange: %v", err)
	}
	if !strings.Contains(spy.rawBody, `"vaultAddress":"0x00000000000000000000000000000000deadbeef"`) {
		t.Errorf("configured vault not echoed in payload: %s", spy.rawBody)
	}
}

// TestRawExchangeRefusesRedirect pins SEC-REDIRECT-001 on the signing path:
// a 3xx from the configured host must not be followed, because the redirected
// POST would re-send the signed action to a host the operator never
// configured. The transport property is shared with RawInfo
// (transport_test.go); this pins it through RawExchange itself.
func TestRawExchangeRefusesRedirect(t *testing.T) {
	var redirectTargetHits int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectTargetHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer target.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/exchange", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	c := &Client{BaseURL: srv.URL, http: hardenedHTTPClient(), key: key}
	_, err = c.RawExchange(context.Background(), testAction{Type: "testAction"})
	if redirectTargetHits != 0 {
		t.Errorf("redirect was followed: %d requests reached the redirect target", redirectTargetHits)
	}
	if err == nil || !strings.Contains(err.Error(), "307") {
		t.Errorf("a 307 must surface as a status error, got %v", err)
	}
}

func TestRawExchangeHTTPError(t *testing.T) {
	spy := newExchangeSpy(t)
	spy.status = http.StatusUnprocessableEntity
	spy.response = `{"detail":"bad action"}`
	c, err := New(context.Background(), testConfig(t, spy.server.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = c.RawExchange(context.Background(), testAction{Type: "testAction"})
	if err == nil || !strings.Contains(err.Error(), "status 422") {
		t.Errorf("expected status error, got %v", err)
	}
}

func TestFloatToWire(t *testing.T) {
	// Expected values produced by hyperliquid-python-sdk float_to_wire.
	cases := []struct {
		in   float64
		want string
	}{
		{4.12, "4.12"},
		{250.5, "250.5"},
		{10, "10"},
		{0.1, "0.1"},
		{100, "100"},
		{0.00000001, "0.00000001"},
		{181.50000000, "181.5"},
		{0, "0"},
		// Documented D-15 divergence: Python's -0 guard is dead code and
		// yields "-0"; the Go SDK's normalization (kept here) yields "0".
		{math.Copysign(0, -1), "0"},
	}
	for _, tc := range cases {
		got, err := FloatToWire(tc.in)
		if err != nil {
			t.Errorf("FloatToWire(%v) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("FloatToWire(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}

	if _, err := FloatToWire(250.500000001); err == nil {
		t.Error("FloatToWire(250.500000001) must fail: rounding loses information")
	}
}
