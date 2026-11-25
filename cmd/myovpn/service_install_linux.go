//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
)

const systemdUnitPath = "/etc/systemd/system/myovpnd.service"

func installService() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("install-service must be run as root")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	targetBin := "/usr/local/bin/myovpn"
	if exe != "/usr/bin/myovpn" && exe != targetBin {
		data, err := os.ReadFile(exe)
		if err != nil {
			return err
		}
		if err := os.WriteFile(targetBin, data, 0o755); err != nil {
			return err
		}
	} else {
		targetBin = exe
	}

	unit := fmt.Sprintf(`[Unit]
Description=MyOVPN Privileged Helper Daemon
After=network-online.target NetworkManager.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s daemon
ExecStopPost=%s cleanup
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
`, targetBin, targetBin)

	if err := os.WriteFile(systemdUnitPath, []byte(unit), 0o644); err != nil {
		return err
	}
	_ = exec.Command("groupadd", "-f", "myovpn").Run()
	_ = exec.Command("systemctl", "daemon-reload").Run()
	return exec.Command("systemctl", "enable", "--now", "myovpnd.service").Run()
}

func uninstallService() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("uninstall-service must be run as root")
	}
	_ = exec.Command("systemctl", "disable", "--now", "myovpnd.service").Run()
	_ = os.Remove(systemdUnitPath)
	_ = exec.Command("systemctl", "daemon-reload").Run()
	return nil
}
