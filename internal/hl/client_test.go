package hl

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/edkdev/hyperliquid-mcp-go/internal/config"
)

const (
	metaFixture  = `{"universe":[{"name":"BTC","szDecimals":5,"maxLeverage":40},{"name":"ETH","szDecimals":4,"maxLeverage":25}],"marginTables":[]}`
	spotFixture  = `{"tokens":[],"universe":[]}`
	simulatedRTT = 25 * time.Millisecond
)

// infoSpy serves the /info endpoint, counting requests per type and
// simulating a realistic network round trip.
type infoSpy struct {
	metaCount atomic.Int32
	spotCount atomic.Int32
	server    *httptest.Server
}

func newInfoSpy(t testing.TB, latency time.Duration) *infoSpy {
	t.Helper()
	spy := &infoSpy{}
	spy.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Type string `json:"type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if latency > 0 {
			time.Sleep(latency)
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Type {
		case "meta":
			spy.metaCount.Add(1)
			_, _ = w.Write([]byte(metaFixture))
		case "spotMeta":
			spy.spotCount.Add(1)
			_, _ = w.Write([]byte(spotFixture))
		default:
			http.Error(w, "unexpected info type: "+req.Type, http.StatusBadRequest)
		}
	}))
	t.Cleanup(spy.server.Close)
	return spy
}

func testConfig(t testing.TB, baseURL string) config.Config {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return config.Config{
		PrivateKey:     key,
		WalletAddress:  crypto.PubkeyToAddress(key.PublicKey).Hex(),
		AccountAddress: crypto.PubkeyToAddress(key.PublicKey).Hex(),
		BaseURL:        baseURL,
	}
}

// TestNewFetchesMetaOnce guards startup latency: meta and spotMeta are
// identical for every consumer, so New must fetch each exactly once —
// constructing a standalone Info alongside NewExchange (which builds its own
// Info internally) would double the startup round trips. Measured: 4 fetches
// ≈ 102ms vs 2 fetches ≈ 51ms at a simulated 25ms RTT (BenchmarkNew).
func TestNewFetchesMetaOnce(t *testing.T) {
	spy := newInfoSpy(t, simulatedRTT)

	start := time.Now()
	client, err := New(context.Background(), testConfig(t, spy.server.URL))
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if client == nil || client.Info == nil || client.Exchange == nil {
		t.Fatal("New returned incomplete client")
	}
	if got := spy.metaCount.Load(); got != 1 {
		t.Errorf("meta fetched %d times, want 1 (%.0fms elapsed, %.0fms floor for sequential fetches)",
			got, float64(elapsed.Milliseconds()), float64(got)*float64(simulatedRTT.Milliseconds()))
	}
	if got := spy.spotCount.Load(); got != 1 {
		t.Errorf("spotMeta fetched %d times, want 1", got)
	}
	t.Logf("startup: %d total requests in %s", spy.metaCount.Load()+spy.spotCount.Load(), elapsed)
}

// BenchmarkNew measures startup wall time against a simulated network.
func BenchmarkNew(b *testing.B) {
	spy := newInfoSpy(b, simulatedRTT)
	cfg := testConfig(b, spy.server.URL)

	b.ReportAllocs()
	for b.Loop() {
		client, err := New(context.Background(), cfg)
		if err != nil {
			b.Fatalf("New: %v", err)
		}
		_ = client
	}
}

// BenchmarkRawInfoConcurrent measures connection reuse under fan-out: mcp-go
// serves tool calls concurrently, and every tool call hits the same API host.
// If the transport's idle-pool-per-host is smaller than the fan-out, each
// burst pays fresh TCP+TLS handshakes (~1 RTT each). Reported as dials/op.
func BenchmarkRawInfoConcurrent(b *testing.B) {
	const fanout = 8

	var dials atomic.Int64
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(simulatedRTT) // keep conns busy so fan-out truly overlaps
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			dials.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, http: hardenedHTTPClient()}
	payload := map[string]any{"type": "allMids", "dex": ""}

	var iterations atomic.Int64
	b.ReportAllocs()
	for b.Loop() {
		iterations.Add(1)
		var wg sync.WaitGroup
		wg.Add(fanout)
		for range fanout {
			go func() {
				defer wg.Done()
				if _, err := c.RawInfo(context.Background(), payload); err != nil {
					b.Errorf("RawInfo: %v", err)
				}
			}()
		}
		wg.Wait()
	}
	b.ReportMetric(float64(dials.Load())/float64(iterations.Load()), "dials/op")
}

// BenchmarkRawInfo measures per-call client-side overhead (no network
// latency): the number that matters is how far below a real API round trip
// (~100ms) this sits.
func BenchmarkRawInfo(b *testing.B) {
	body := []byte(`{"marginSummary":{"accountValue":"10000.0","totalMarginUsed":"500.0"},"assetPositions":[{"position":{"coin":"BTC","szi":"0.5"}},{"position":{"coin":"ETH","szi":"2.0"}}]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, http: srv.Client()}
	payload := map[string]any{"type": "clearinghouseState", "user": "0xabc"}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := c.RawInfo(context.Background(), payload); err != nil {
			b.Fatalf("RawInfo: %v", err)
		}
	}
}
