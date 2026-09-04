package hl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestNoGoroutineLeak proves RawInfo (the only client path that runs per tool
// call) leaves no goroutines behind: HTTP/2 idle connections park their
// read/write loops, so idle conns are closed before counting. Any growth
// after settle is a leak.
func TestNoGoroutineLeak(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, http: hardenedHTTPClient()}
	payload := map[string]any{"type": "allMids", "dex": ""}

	call := func() {
		if _, err := c.RawInfo(context.Background(), payload); err != nil {
			t.Errorf("RawInfo: %v", err)
		}
	}

	// Warmup: establish the steady state (transports, pooled structures).
	for range 10 {
		call()
	}
	c.http.CloseIdleConnections()
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	before := runtime.NumGoroutine()

	// Sequential and concurrent bursts.
	for range 100 {
		call()
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(8)
		for range 8 {
			go func() {
				defer wg.Done()
				call()
			}()
		}
		wg.Wait()
	}

	c.http.CloseIdleConnections()
	runtime.GC()
	// Poll instead of a fixed sleep: connection teardown goroutines finish
	// asynchronously, and a loaded machine can lag a fixed sleep (the retry
	// pattern goleak uses). Fail only if the count is still high at the
	// deadline.
	after := before
	for deadline := time.Now().Add(2 * time.Second); ; {
		after = runtime.NumGoroutine()
		if after <= before {
			break
		}
		if time.Now().After(deadline) {
			t.Errorf("goroutine leak: before=%d after=%d (+%d)", before, after, after-before)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
}
