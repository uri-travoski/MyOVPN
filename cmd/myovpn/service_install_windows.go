//go:build windows

package main

import (
	"os"

	"github.com/uri-travoski/MyOVPN/internal/winservice"
)

func installService() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return winservice.InstallAndStart(exe)
}

func uninstallService() error {
	return winservice.Uninstall()
}
