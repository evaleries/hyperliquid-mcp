package hl

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Transport hardening in client.go carries security properties that no other
// test observes: a refactor to plain http.DefaultClient or a bare
// io.ReadAll(resp.Body) would silently remove them with the rest of the suite
// still green. These tests pin the observable behavior of each one.

// TestRawInfoRefusesRedirect pins SEC-REDIRECT-001: a 3xx must not be
// followed, because the redirected POST would re-send the request body — for
// /exchange, a signed action — to a host the operator never configured.
func TestRawInfoRefusesRedirect(t *testing.T) {
	var redirectTargetHits int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectTargetHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"attacker":"controlled"}`))
	}))
	defer target.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/info", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, http: hardenedHTTPClient()}
	raw, err := c.RawInfo(context.Background(), map[string]any{"type": "allMids"})
	if redirectTargetHits != 0 {
		t.Errorf("redirect was followed: %d requests reached the redirect target", redirectTargetHits)
	}
	if err == nil {
		t.Fatalf("a 307 must surface as an error, got body %s", raw)
	}
	if !strings.Contains(err.Error(), "307") {
		t.Errorf("error should name the status: %v", err)
	}
}

// TestRawInfoBodySizeCap pins SEC-DOS-002 at its boundary: a body at the cap
// is returned, one byte past it is rejected instead of being buffered or
// forwarded into the MCP client's context.
func TestRawInfoBodySizeCap(t *testing.T) {
	const cap = 1 << 10
	restore := maxInfoResponseBytes
	maxInfoResponseBytes = cap
	t.Cleanup(func() { maxInfoResponseBytes = restore })

	var bodyLen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// A JSON string of the requested total length: valid JSON at any size.
		_, _ = w.Write([]byte(`"` + strings.Repeat("x", bodyLen-2) + `"`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, http: hardenedHTTPClient()}

	bodyLen = cap
	raw, err := c.RawInfo(context.Background(), map[string]any{"type": "allMids"})
	if err != nil {
		t.Fatalf("a body exactly at the cap must be accepted: %v", err)
	}
	if len(raw) != cap {
		t.Errorf("body length = %d, want %d", len(raw), cap)
	}

	bodyLen = cap + 1
	if _, err := c.RawInfo(context.Background(), map[string]any{"type": "allMids"}); err == nil {
		t.Fatal("a body one byte past the cap must be rejected")
	} else if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error should report the cap: %v", err)
	}
}

// TestRawInfoStopsReadingPastCap is the other half of SEC-DOS-002: rejecting
// an oversized body is not enough if the whole body was buffered first. The
// server streams far more than the cap and reports how much it managed to
// push before the client hung up; an unbounded reader consumes all of it.
func TestRawInfoStopsReadingPastCap(t *testing.T) {
	const (
		cap   = 1 << 10
		chunk = 64 << 10
		total = 64 << 20
	)
	restore := maxInfoResponseBytes
	maxInfoResponseBytes = cap
	t.Cleanup(func() { maxInfoResponseBytes = restore })

	served := make(chan int, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := bytes.Repeat([]byte("x"), chunk)
		written := 0
		for written < total {
			n, err := w.Write(payload)
			written += n
			if err != nil { // client stopped reading and closed the body
				break
			}
		}
		served <- written
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, http: hardenedHTTPClient()}
	if _, err := c.RawInfo(context.Background(), map[string]any{"type": "allMids"}); err == nil {
		t.Fatal("an oversized body must be rejected")
	}

	// Socket and handler buffers let a few hundred KiB through before the
	// write fails; the margin to 64 MiB is what distinguishes a bounded read
	// from a full one.
	const bounded = 8 << 20
	if written := <-served; written >= bounded {
		t.Errorf("read %d bytes of a %d-byte body: the cap did not bound buffering", written, total)
	}
}

// TestRawInfoNonOKStatusCarriesTruncatedBody: API errors arrive as HTTP
// errors with a body worth reporting, but the body is untrusted input headed
// for an LLM context, so the excerpt stays bounded.
func TestRawInfoNonOKStatusCarriesTruncatedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(strings.Repeat("x", 4096)))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, http: hardenedHTTPClient()}
	_, err := c.RawInfo(context.Background(), map[string]any{"type": "allMids"})
	if err == nil {
		t.Fatal("non-200 must error")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should name the status: %v", err)
	}
	if len(err.Error()) > 512 {
		t.Errorf("response excerpt not truncated: error is %d chars", len(err.Error()))
	}
	if !strings.HasSuffix(err.Error(), "...") {
		t.Errorf("truncation marker missing: %v", err)
	}
}

// TestRawInfoEncodeFailureNeverDials: an unencodable payload is a programming
// error, and it must fail before a request is put on the wire.
func TestRawInfoEncodeFailureNeverDials(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, http: hardenedHTTPClient()}
	if _, err := c.RawInfo(context.Background(), map[string]any{"bad": func() {}}); err == nil {
		t.Fatal("unencodable payload must error")
	}
	if requests != 0 {
		t.Errorf("request reached the server despite an encode failure: %d", requests)
	}
}
