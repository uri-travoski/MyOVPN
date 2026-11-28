//go:build linux

package tray

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/uri-travoski/MyOVPN/internal/config"
)

func TestRunManageAppsDialogRequiresDisplay(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	_, saved, err := RunManageAppsDialog(nil)
	if err == nil || saved {
		t.Fatalf("expected error without DISPLAY or WAYLAND_DISPLAY, got saved=%v err=%v", saved, err)
	}
}

func TestAppendSelectedAppPathDesktopAndBinary(t *testing.T) {
	tmp := t.TempDir()
	desktopPath := filepath.Join(tmp, "chromium-vpn.desktop")
	content := "[Desktop Entry]\nType=Application\nName=Chromium Custom\nExec=/usr/lib/chromium/chromium --user-data-dir=/tmp/p1 %U\n"
	if err := os.WriteFile(desktopPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write desktop: %v", err)
	}

	updated, saved, err := appendSelectedAppPath(desktopPath, []config.AppEntry{{ID: "firefox", Name: "Firefox", Command: "firefox"}})
	if err != nil || !saved || len(updated) != 2 {
		t.Fatalf("expected saved 2 apps from .desktop, got len=%d saved=%v err=%v", len(updated), saved, err)
	}
	if updated[1].Name != "Chromium Custom" || updated[1].Command != "/usr/lib/chromium/chromium" {
		t.Fatalf("unexpected desktop parsed entry: %+v", updated[1])
	}

	binUpdated, saved, err := appendSelectedAppPath("/opt/antigravity/antigravity", updated)
	if err != nil || !saved || len(binUpdated) != 3 {
		t.Fatalf("expected saved 3 apps from binary path, got len=%d saved=%v err=%v", len(binUpdated), saved, err)
	}
}
