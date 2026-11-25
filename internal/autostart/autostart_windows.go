//go:build windows

package autostart

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	valueName  = "MyOVPN"
)

// Sync sets or removes the Windows HKCU Run registry key pointing to the current executable path (FR-17).
func Sync(enabled bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if !enabled {
		err := k.DeleteValue(valueName)
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(valueName, fmt.Sprintf(`"%s"`, exe))
}

func InstallChromeVPNLauncher() error { return nil }
