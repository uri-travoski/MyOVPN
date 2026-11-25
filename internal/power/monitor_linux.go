//go:build linux

package power

import (
	"context"

	"github.com/godbus/dbus/v5"
)

// WatchResumeAndNetwork listens for systemd-logind PrepareForSleep(false) and NetworkManager StateChanged
// over the system D-Bus and invokes onEvent whenever the system resumes or changes networks.
func WatchResumeAndNetwork(ctx context.Context, onEvent func(reason string)) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return
	}
	defer conn.Close()

	_ = conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.login1.Manager"),
		dbus.WithMatchMember("PrepareForSleep"),
	)
	_ = conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.NetworkManager"),
		dbus.WithMatchMember("StateChanged"),
	)

	ch := make(chan *dbus.Signal, 16)
	conn.Signal(ch)

	for {
		select {
		case <-ctx.Done():
			return
		case sig, ok := <-ch:
			if !ok || sig == nil {
				return
			}
			switch sig.Name {
			case "org.freedesktop.login1.Manager.PrepareForSleep":
				if len(sig.Body) > 0 {
					if sleeping, ok := sig.Body[0].(bool); ok && !sleeping {
						onEvent("resume from suspend")
					}
				}
			case "org.freedesktop.NetworkManager.StateChanged":
				onEvent("network state changed")
			}
		}
	}
}
