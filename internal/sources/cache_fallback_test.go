package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/uri-travoski/MyOVPN/internal/core"
)

func TestFetchAllCacheFallbackAndMirrors(t *testing.T) {
	tmp := t.TempDir()
	cachePath := filepath.Join(tmp, "servers.json")
	speed := int64(25_000_000)
	uptime := int64(3_600_000)
	ping := 15
	sessions := 1

	err := core.SaveServerCache(cachePath, core.CachedServerList{
		FetchedAt: time.Now().Add(-30 * time.Minute), // expired (> 15m)
		Source:    "VPN Gate",
		Servers: []core.Server{
			{
				ID:          "cached-jp-1",
				Source:      "vpngate",
				SourceTag:   "VG",
				CountryCode: "JP",
				CountryName: "Japan",
				IP:          "203.0.113.10",
				SpeedBps:    &speed,
				UptimeMs:    &uptime,
				PingMs:      &ping,
				Sessions:    &sessions,
				OVPNText:    "remote 203.0.113.10 443\n",
			},
		},
	})
	if err != nil {
		t.Fatalf("save cache: %v", err)
	}

	// Simulate all network requests failing with 503
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	// Disable online sources except cache, or point custom_url to failing server -> must fall back to cache
	res := FetchAll(
		context.Background(),
		srv.Client(),
		[]string{"custom_url", "cache"},
		map[string]bool{"custom_url": true, "cache": true},
		[]string{srv.URL},
		"",
		cachePath,
		true,
	)

	if !res.UsingCache {
		t.Fatalf("expected UsingCache=true on fallback, got false")
	}
	if len(res.Servers) != 1 || res.Servers[0].ID != "cached-jp-1" {
		t.Fatalf("expected cached server fallback, got %+v", res.Servers)
	}
}
