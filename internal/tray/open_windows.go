//go:build windows

package tray

import (
	"os/exec"
	"syscall"

	"github.com/uri-travoski/MyOVPN/internal/winservice"
)

func openPath(path string) {
	cmd := exec.Command("cmd", "/c", "start", "", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Start()
}

func installHelperElevated() error {
	return winservice.ElevateAndRun("install-service")
}

func removeHelperElevated() error {
	return winservice.ElevateAndRun("uninstall-service")
}
