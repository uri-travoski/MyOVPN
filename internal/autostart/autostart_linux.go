//go:build linux

package autostart

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/godbus/dbus/v5"
	"github.com/uri-travoski/MyOVPN/internal/config"
)

const desktopFilename = "myovpn.desktop"

func userAutostartPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", desktopFilename)
}

func systemAutostartExists() bool {
	_, err := os.Stat(filepath.Join("/etc/xdg/autostart", desktopFilename))
	return err == nil
}

// Sync updates the Linux autostart entry according to enabled (FR-17).
func Sync(enabled bool) error {
	if config.IsFlatpak() {
		return syncFlatpakBackgroundPortal(enabled)
	}

	userPath := userAutostartPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		return err
	}

	execPath := "/usr/bin/myovpn"
	if config.IsAppImage() {
		execPath = os.Getenv("APPIMAGE")
	} else if p, err := os.Executable(); err == nil && p != "" {
		execPath = p
	}

	if !enabled {
		if systemAutostartExists() {
			// Override system-wide /etc/xdg/autostart entry cleanly per XDG spec
			content := "[Desktop Entry]\nType=Application\nName=MyOVPN\nHidden=true\n"
			return os.WriteFile(userPath, []byte(content), 0o644)
		}
		err := os.Remove(userPath)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	// Enabled
	if systemAutostartExists() && !config.IsAppImage() {
		// Remove any user Hidden=true override so /etc/xdg/autostart takes effect
		_ = os.Remove(userPath)
		return nil
	}

	content := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=MyOVPN
Comment=One-click free OpenVPN tray app
Exec=%q
Icon=myovpn
Terminal=false
Categories=Network;Utility;
StartupNotify=false
X-GNOME-Autostart-enabled=true
`, execPath)
	return os.WriteFile(userPath, []byte(content), 0o644)
}

func syncFlatpakBackgroundPortal(enabled bool) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer conn.Close()

	obj := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	options := map[string]dbus.Variant{
		"reason":    dbus.MakeVariant("Start MyOVPN tray icon on login"),
		"autostart": dbus.MakeVariant(enabled),
		"commandline": dbus.MakeVariant([]string{
			"myovpn",
		}),
	}
	return obj.Call("org.freedesktop.portal.Background.RequestBackground", 0, "", options).Err
}

// InstallChromeVPNLauncher installs the "Chrome (VPN)" desktop shortcut for per-app mode.
func InstallChromeVPNLauncher() error {
	if config.IsFlatpak() {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	appsDir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		return err
	}
	execPath := "/usr/bin/myovpn"
	if config.IsAppImage() {
		execPath = os.Getenv("APPIMAGE")
	} else if p, err := os.Executable(); err == nil && p != "" {
		execPath = p
	}
	chromeDesktop := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Chrome (VPN)
Comment=Launch Google Chrome inside the MyOVPN VPN namespace
Exec=%q launch chrome
Icon=google-chrome
Terminal=false
Categories=Network;WebBrowser;
StartupWMClass=ChromeVPN
`, execPath)
	if err := os.WriteFile(filepath.Join(appsDir, "myovpn-chrome.desktop"), []byte(chromeDesktop), 0o644); err != nil {
		return err
	}
	chromiumDesktop := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Chromium (VPN)
Comment=Launch Chromium inside the MyOVPN VPN namespace
Exec=%q launch chromium
Icon=chromium
Terminal=false
Categories=Network;WebBrowser;
StartupWMClass=ChromiumVPN
`, execPath)
	return os.WriteFile(filepath.Join(appsDir, "myovpn-chromium.desktop"), []byte(chromiumDesktop), 0o644)
}
