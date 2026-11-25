//go:build linux

package tray

import (
	"os"
	"os/exec"
)

func openPath(path string) {
	_ = exec.Command("xdg-open", path).Start()
}

func installHelperElevated() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return exec.Command("pkexec", exe, "install-service").Run()
}

func removeHelperElevated() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return exec.Command("pkexec", exe, "uninstall-service").Run()
}
