//go:build linux

package netns

import (
	"slices"
	"testing"

	"github.com/uri-travoski/MyOVPN/internal/config"
)

func TestPrepareLaunchEnvAndArgsWayland(t *testing.T) {
	app := config.AppEntry{
		ID:      "chromium",
		Name:    "Chromium (VPN)",
		Command: "/usr/lib/chromium/chromium",
		Args:    []string{"--user-data-dir=/home/owner/Applications/Chromium/profile-1"},
	}
	inEnv := []string{
		"WAYLAND_DISPLAY=wayland-0",
		"XDG_RUNTIME_DIR=/run/user/1000",
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus",
	}

	outEnv, outArgs := PrepareLaunchEnvAndArgs(inEnv, app)

	for _, want := range []string{
		"XDG_SESSION_TYPE=wayland",
		"ELECTRON_OZONE_PLATFORM_HINT=auto",
		"MOZ_ENABLE_WAYLAND=1",
		"QT_QPA_PLATFORM=wayland;xcb",
	} {
		if !slices.Contains(outEnv, want) {
			t.Fatalf("missing %q in Wayland launch env: %v", want, outEnv)
		}
	}
	if !slices.Contains(outArgs, "--ozone-platform-hint=auto") {
		t.Fatalf("expected --ozone-platform-hint=auto for Chromium on Wayland, got %v", outArgs)
	}
}

func TestPrepareLaunchEnvAndArgsRespectsExplicitUserFlags(t *testing.T) {
	app := config.AppEntry{
		ID:      "chromium",
		Command: "/usr/bin/google-chrome",
		Args:    []string{"--ozone-platform=x11"},
	}
	inEnv := []string{
		"WAYLAND_DISPLAY=wayland-0",
		"DISPLAY=:0",
		"MOZ_ENABLE_WAYLAND=0",
	}

	outEnv, outArgs := PrepareLaunchEnvAndArgs(inEnv, app)
	if slices.Contains(outArgs, "--ozone-platform-hint=auto") {
		t.Fatalf("did not expect extra ozone hint when --ozone-platform=x11 is set: %v", outArgs)
	}
	if !slices.Contains(outEnv, "MOZ_ENABLE_WAYLAND=0") || slices.Contains(outEnv, "MOZ_ENABLE_WAYLAND=1") {
		t.Fatalf("explicit MOZ_ENABLE_WAYLAND=0 must be preserved: %v", outEnv)
	}
}

func TestPrepareLaunchEnvAndArgsX11(t *testing.T) {
	app := config.AppEntry{
		ID:      "chromium",
		Command: "/usr/lib/chromium/chromium",
		Args:    []string{"--user-data-dir=/tmp/prof"},
	}
	inEnv := []string{
		"DISPLAY=:0",
		"XAUTHORITY=/home/owner/.Xauthority",
	}

	outEnv, outArgs := PrepareLaunchEnvAndArgs(inEnv, app)
	if !slices.Contains(outEnv, "XDG_SESSION_TYPE=x11") {
		t.Fatalf("expected XDG_SESSION_TYPE=x11 for X11 session, got %v", outEnv)
	}
	if slices.Contains(outArgs, "--ozone-platform-hint=auto") {
		t.Fatalf("unexpected Wayland ozone flag on X11-only session: %v", outArgs)
	}
}
