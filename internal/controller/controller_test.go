package controller

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/uri-travoski/MyOVPN/internal/config"
	"github.com/uri-travoski/MyOVPN/internal/core"
)

type recordingSink struct {
	notices []string
}

func (s *recordingSink) OnNotify(title, body string) {
	s.notices = append(s.notices, title+": "+body)
}

func (s *recordingSink) OnStatus(_ core.StatusSnapshot) {}

func TestControllerRefreshAndStatusFromCache(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	cachePath := filepath.Join(tmp, "servers.json")
	regPath := filepath.Join(tmp, "profiles.json")

	speed := int64(50_000_000)
	uptime := int64(3_600_000)
	ping := 20
	sess := 1

	if err := core.SaveServerCache(cachePath, core.CachedServerList{
		FetchedAt: time.Now(),
		Source:    "VPN Gate",
		Servers: []core.Server{
			{
				ID:          "jp-1",
				Source:      "vpngate",
				SourceTag:   "VG",
				CountryCode: "JP",
				CountryName: "Japan",
				IP:          "198.51.100.1",
				SpeedBps:    &speed,
				UptimeMs:    &uptime,
				PingMs:      &ping,
				Sessions:    &sess,
				OVPNText:    "remote 198.51.100.1 443\n",
			},
		},
	}); err != nil {
		t.Fatalf("save cache: %v", err)
	}

	cfg := config.DefaultConfig()
	sink := &recordingSink{}
	ctrl := New(cfg, cfgPath, cachePath, regPath, sink, nil)

	snap := ctrl.Refresh(context.Background(), false)
	if snap.State != core.StateDisconnected {
		t.Fatalf("expected disconnected on startup, got %s", snap.State)
	}
	if len(snap.Countries) != 1 || snap.Countries[0].Code != "JP" {
		t.Fatalf("expected JP in countries, got %+v", snap.Countries)
	}

	ctrl.notify("MyOVPN", "Test notice")
	snap2 := ctrl.Status()
	if snap2.Notice == nil || snap2.Notice.Seq != 1 || snap2.Notice.Body != "Test notice" {
		t.Fatalf("expected notice seq=1 in status snapshot, got %+v", snap2.Notice)
	}
}
