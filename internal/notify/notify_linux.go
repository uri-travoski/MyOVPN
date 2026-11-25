//go:build linux

package notify

import (
	"os/exec"

	"github.com/godbus/dbus/v5"
)

// Send displays a desktop notification via org.freedesktop.Notifications (with notify-send fallback).
func Send(title, body string) {
	conn, err := dbus.ConnectSessionBus()
	if err == nil {
		defer conn.Close()
		obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
		call := obj.Call(
			"org.freedesktop.Notifications.Notify",
			0,
			"MyOVPN",
			uint32(0),
			"myovpn",
			title,
			body,
			[]string{},
			map[string]dbus.Variant{},
			int32(5000),
		)
		if call.Err == nil {
			return
		}
	}
	if _, err := exec.LookPath("notify-send"); err == nil {
		_ = exec.Command("notify-send", "-a", "MyOVPN", title, body).Start()
	}
}
