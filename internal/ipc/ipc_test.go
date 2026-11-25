package ipc

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/uri-travoski/MyOVPN/internal/config"
	"github.com/uri-travoski/MyOVPN/internal/controller"
	"github.com/uri-travoski/MyOVPN/internal/core"
)

func TestFilterSessionEnvDropsUnsafeKeys(t *testing.T) {
	in := map[string]string{
		"DISPLAY":                  ":0",
		"WAYLAND_DISPLAY":          "wayland-0",
		"LD_PRELOAD":               "/tmp/evil.so",
		"BASH_ENV":                 "/tmp/evil.sh",
		"DBUS_SESSION_BUS_ADDRESS": "unix:path=/run/user/1000/bus",
	}
	out := FilterSessionEnv(in)
	if len(out) != 3 {
		t.Fatalf("expected 3 safe env vars, got %v", out)
	}
	for _, entry := range out {
		if entry == "LD_PRELOAD=/tmp/evil.so" || entry == "BASH_ENV=/tmp/evil.sh" {
			t.Fatalf("unsafe env var leaked: %s", entry)
		}
	}
}

func TestIPCServerDispatchStatusAndDisconnect(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	cachePath := filepath.Join(tmp, "servers.json")
	regPath := filepath.Join(tmp, "profiles.json")
	sockPath := filepath.Join(tmp, "test.sock")

	speed := int64(20_000_000)
	uptime := int64(3_600_000)
	ping := 25
	sess := 2
	if err := core.SaveServerCache(cachePath, core.CachedServerList{
		FetchedAt: time.Now(),
		Source:    "VPN Gate",
		Servers: []core.Server{{
			ID: "jp-test", Source: "vpngate", SourceTag: "VG",
			CountryCode: "JP", CountryName: "Japan", IP: "198.51.100.2",
			SpeedBps: &speed, UptimeMs: &uptime, PingMs: &ping, Sessions: &sess,
			OVPNText: "remote 198.51.100.2 443\n",
		}},
	}); err != nil {
		t.Fatalf("save cache: %v", err)
	}

	cfg := config.DefaultConfig()
	ctrl := controller.New(cfg, cfgPath, cachePath, regPath, nil, nil)
	srv := NewServer(ctrl, cfg, cfgPath, nil)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx, ln) }()

	// Refresh via direct dispatch
	resp := srv.dispatch(ctx, CallerInfo{}, Request{Command: CmdRefresh})
	if !resp.OK || resp.Status == nil || len(resp.Status.Countries) != 1 {
		t.Fatalf("unexpected refresh resp: %+v", resp)
	}

	// Unknown command must be rejected
	bad := srv.dispatch(ctx, CallerInfo{}, Request{Command: "exec /bin/sh"})
	if bad.OK || bad.Error == "" {
		t.Fatalf("expected unknown command rejection, got %+v", bad)
	}

	// Disconnect succeeds cleanly
	disc := srv.dispatch(ctx, CallerInfo{}, Request{Command: CmdDisconnect})
	if !disc.OK || disc.Status == nil || disc.Status.State != core.StateDisconnected {
		t.Fatalf("unexpected disconnect resp: %+v", disc)
	}
}
