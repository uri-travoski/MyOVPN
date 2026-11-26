package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	AppName      = "MyOVPN"
	BinaryName   = "myovpn"
	HelperName   = "myovpnd"
	ProfilePrefx = "myovpn-"
	NetnsName    = "myovpn"
)

// AppEntry defines an application that can be launched inside the VPN namespace.
type AppEntry struct {
	ID      string   `toml:"id" json:"id"`
	Name    string   `toml:"name" json:"name"`
	Command string   `toml:"command" json:"command"`
	Args    []string `toml:"args" json:"args"`
}

// Config holds user and runtime configuration stored in config.toml.
type Config struct {
	Mode                   string          `toml:"mode" json:"mode"`
	Autostart              bool            `toml:"autostart" json:"autostart"`
	Notifications          bool            `toml:"notifications" json:"notifications"`
	AutoLaunchApp          bool            `toml:"auto_launch_app" json:"auto_launch_app"`
	FirstRunNoticeShown    bool            `toml:"first_run_notice_shown" json:"first_run_notice_shown"`
	RecentCountries        []string        `toml:"recent_countries" json:"recent_countries"`
	MaxProfiles            int             `toml:"max_profiles" json:"max_profiles"`
	MinSpeedMbps           float64         `toml:"min_speed_mbps" json:"min_speed_mbps"`
	MinUptimeMinutes       int             `toml:"min_uptime_minutes" json:"min_uptime_minutes"`
	BlacklistMinutes       int             `toml:"blacklist_minutes" json:"blacklist_minutes"`
	ConnectTimeoutSec      int             `toml:"connect_timeout_sec" json:"connect_timeout_sec"`
	HealthCheckURL         string          `toml:"health_check_url" json:"health_check_url"`
	HealthCheckIntervalSec int             `toml:"health_check_interval_sec" json:"health_check_interval_sec"`
	HealthCheckTimeoutSec  int             `toml:"health_check_timeout_sec" json:"health_check_timeout_sec"`
	GeoIPCheckEnabled      bool            `toml:"geoip_check_enabled" json:"geoip_check_enabled"`
	GeoIPURL               string          `toml:"geoip_url" json:"geoip_url"`
	SourceOrder            []string        `toml:"source_order" json:"source_order"`
	EnabledSources         map[string]bool `toml:"enabled_sources" json:"enabled_sources"`
	CustomURLs             []string        `toml:"custom_urls" json:"custom_urls"`
	ImportDir              string          `toml:"import_dir" json:"import_dir"`
	Apps                   []AppEntry      `toml:"apps" json:"apps"`
}

// DefaultConfig returns a Config populated with PRD defaults.
func DefaultConfig() *Config {
	defaultMode := "per-app"
	if runtime.GOOS == "windows" || IsFlatpak() {
		defaultMode = "system-wide"
	}
	cfgDir := ConfigDir()
	return &Config{
		Mode:                   defaultMode,
		Autostart:              true,
		Notifications:          true,
		AutoLaunchApp:          true,
		FirstRunNoticeShown:    false,
		RecentCountries:        []string{},
		MaxProfiles:            3,
		MinSpeedMbps:           5.0,
		MinUptimeMinutes:       30,
		BlacklistMinutes:       60,
		ConnectTimeoutSec:      20,
		HealthCheckURL:         "https://www.gstatic.com/generate_204",
		HealthCheckIntervalSec: 20,
		HealthCheckTimeoutSec:  8,
		GeoIPCheckEnabled:      false,
		GeoIPURL:               "https://ipapi.co/country/",
		SourceOrder: []string{
			"vpngate",
			"vpngate_mirrors",
			"vpnbook",
			"freeopenvpn",
			"vpnobratno",
			"cache",
			"imported",
			"custom_url",
		},
		EnabledSources: map[string]bool{
			"vpngate":         true,
			"vpngate_mirrors": true,
			"vpnbook":         true,
			"freeopenvpn":     false,
			"vpnobratno":      false,
			"cache":           true,
			"imported":        true,
			"custom_url":      true,
		},
		CustomURLs: []string{},
		ImportDir:  filepath.Join(cfgDir, "imported"),
		Apps:       defaultApps(cfgDir),
	}
}

func defaultApps(cfgDir string) []AppEntry {
	if runtime.GOOS == "windows" {
		return []AppEntry{}
	}
	return []AppEntry{
		{
			ID:      "chrome",
			Name:    "Chrome (VPN)",
			Command: "google-chrome",
			Args: []string{
				"--user-data-dir=" + filepath.Join(cfgDir, "chrome-profile"),
				"--class=ChromeVPN",
				"--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
				"--no-first-run",
			},
		},
		{
			ID:      "chromium",
			Name:    "Chromium (VPN)",
			Command: "/usr/lib/chromium/chromium",
			Args: []string{
				"--user-data-dir=" + filepath.Join(cfgDir, "chromium-profile"),
				"--class=ChromiumVPN",
				"--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
				"--no-first-run",
			},
		},
	}
}

// IsFlatpak returns true when running inside a Flatpak sandbox.
func IsFlatpak() bool {
	_, err := os.Stat("/.flatpak-info")
	return err == nil
}

// IsAppImage returns true when running from an AppImage bundle.
func IsAppImage() bool {
	return os.Getenv("APPIMAGE") != ""
}

// ConfigDir returns the per-user configuration directory.
func ConfigDir() string {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("APPDATA"); d != "" {
			return filepath.Join(d, AppName)
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "AppData", "Roaming", AppName)
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, BinaryName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", BinaryName)
}

// CacheDir returns the per-user cache directory.
func CacheDir() string {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, AppName, "cache")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "AppData", "Local", AppName, "cache")
	}
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, BinaryName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", BinaryName)
}

// StateDir returns the per-user state/logs directory.
func StateDir() string {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, AppName)
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "AppData", "Local", AppName)
	}
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, BinaryName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", BinaryName)
}

// LogPath returns the path to the main log file.
func LogPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(StateDir(), "logs", BinaryName+".log")
	}
	return filepath.Join(StateDir(), BinaryName+".log")
}

// DaemonLogPath returns the daemon log path when running as a system service.
func DaemonLogPath() string {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("ProgramData"); d != "" {
			return filepath.Join(d, AppName, "logs", HelperName+".log")
		}
		return filepath.Join(StateDir(), "logs", HelperName+".log")
	}
	if os.Geteuid() == 0 {
		return "/var/log/myovpnd.log"
	}
	return filepath.Join(StateDir(), HelperName+".log")
}

// ConfigPath returns the path to config.toml.
func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.toml")
}

// CacheServersPath returns the path to servers.json cache.
func CacheServersPath() string {
	return filepath.Join(CacheDir(), "servers.json")
}

// Load reads config.toml from disk or creates it with defaults if missing.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			_ = Save(path, cfg)
			return cfg, nil
		}
		return cfg, err
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return cfg, err
	}
	cfg.Normalize()
	return cfg, nil
}

// Save writes cfg to path atomically.
func Save(path string, cfg *Config) error {
	cfg.Normalize()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Normalize clamps fields to valid ranges and ensures defaults for missing values.
func (c *Config) Normalize() {
	if c.Mode != "per-app" && c.Mode != "system-wide" {
		if runtime.GOOS == "windows" || IsFlatpak() {
			c.Mode = "system-wide"
		} else {
			c.Mode = "per-app"
		}
	}
	if runtime.GOOS == "windows" || IsFlatpak() {
		c.Mode = "system-wide"
	}
	if c.MaxProfiles < 3 {
		c.MaxProfiles = 3
	}
	if c.MaxProfiles > 5 {
		c.MaxProfiles = 5
	}
	if c.MinSpeedMbps <= 0 {
		c.MinSpeedMbps = 5.0
	}
	if c.MinUptimeMinutes < 0 {
		c.MinUptimeMinutes = 30
	}
	if c.BlacklistMinutes <= 0 {
		c.BlacklistMinutes = 60
	}
	if c.ConnectTimeoutSec <= 0 {
		c.ConnectTimeoutSec = 20
	}
	if strings.TrimSpace(c.HealthCheckURL) == "" {
		c.HealthCheckURL = "https://www.gstatic.com/generate_204"
	}
	if c.HealthCheckIntervalSec <= 0 {
		c.HealthCheckIntervalSec = 20
	}
	if c.HealthCheckTimeoutSec <= 0 {
		c.HealthCheckTimeoutSec = 8
	}
	if c.EnabledSources == nil {
		c.EnabledSources = map[string]bool{}
	}
	if len(c.SourceOrder) == 0 {
		c.SourceOrder = DefaultConfig().SourceOrder
	}
	if c.ImportDir == "" {
		c.ImportDir = filepath.Join(ConfigDir(), "imported")
	}
	if len(c.RecentCountries) > 3 {
		c.RecentCountries = c.RecentCountries[:3]
	}
}

// RecordRecentCountry prepends code to RecentCountries, keeping at most 3 unique uppercase codes.
func (c *Config) RecordRecentCountry(code string) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return
	}
	out := []string{code}
	for _, existing := range c.RecentCountries {
		ex := strings.ToUpper(strings.TrimSpace(existing))
		if ex != "" && ex != code {
			out = append(out, ex)
		}
		if len(out) == 3 {
			break
		}
	}
	c.RecentCountries = out
}
