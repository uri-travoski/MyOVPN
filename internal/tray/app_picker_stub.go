//go:build !linux

package tray

import (
	"fmt"

	"github.com/uri-travoski/MyOVPN/internal/config"
)

func RunManageAppsDialog(_ []config.AppEntry) ([]config.AppEntry, bool, error) {
	return nil, false, fmt.Errorf("per-app GUI picker is only supported on Linux")
}
