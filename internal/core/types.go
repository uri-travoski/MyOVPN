package core

import (
	"fmt"
	"strings"
	"time"
)

// AuthType describes the authentication method required by an OpenVPN server.
type AuthType string

const (
	AuthNone     AuthType = "none"
	AuthUserPass AuthType = "user/pass"
)

// Server is the normalized server record across all sources (FR-2).
type Server struct {
	ID           string   `json:"id"`
	Source       string   `json:"source"`
	SourceTag    string   `json:"source_tag"`
	CountryCode  string   `json:"country_code"`
	CountryName  string   `json:"country_name"`
	IP           string   `json:"ip"`
	Hostname     string   `json:"hostname,omitempty"`
	Port         *int     `json:"port"`
	Proto        string   `json:"proto"`
	SpeedBps     *int64   `json:"speed_bps"`
	PingMs       *int     `json:"ping_ms"`
	Sessions     *int     `json:"sessions"`
	UptimeMs     *int64   `json:"uptime_ms"`
	OVPNText     string   `json:"ovpn_text"`
	Auth         AuthType `json:"auth"`
	Username     string   `json:"username,omitempty"`
	Password     string   `json:"password,omitempty"`
	StrongCipher bool     `json:"strong_cipher"`
	Score        float64  `json:"score"`
}

// CountrySummary represents a country with at least one usable server.
type CountrySummary struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Flag        string `json:"flag"`
	ServerCount int    `json:"server_count"`
	Pinned      bool   `json:"pinned"`
}

// SpeedMbps returns speed in megabits per second, or 0 if unknown.
func (s *Server) SpeedMbps() float64 {
	if s.SpeedBps == nil || *s.SpeedBps <= 0 {
		return 0
	}
	return float64(*s.SpeedBps) / 1_000_000.0
}

// FormatSpeed returns a human-readable speed string.
func (s *Server) FormatSpeed() string {
	if s.SpeedBps == nil {
		return "unknown speed"
	}
	mbps := s.SpeedMbps()
	if mbps >= 10 {
		return fmt.Sprintf("%.0f Mbps", mbps)
	}
	return fmt.Sprintf("%.1f Mbps", mbps)
}

// FormatSessions returns a human-readable current session count string.
func (s *Server) FormatSessions() string {
	if s.Sessions == nil {
		return "n/a users"
	}
	if *s.Sessions == 1 {
		return "1 user"
	}
	return fmt.Sprintf("%d users", *s.Sessions)
}

// FlagEmoji converts an ISO 3166-1 alpha-2 country code to its regional indicator flag emoji.
func FlagEmoji(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 2 {
		return "🌐"
	}
	r1 := rune(code[0])
	r2 := rune(code[1])
	if r1 < 'A' || r1 > 'Z' || r2 < 'A' || r2 > 'Z' {
		return "🌐"
	}
	const base = 0x1F1E6 - 'A'
	return string([]rune{base + r1, base + r2})
}

// ConnectionState represents the current tunnel lifecycle state.
type ConnectionState string

const (
	StateDisconnected ConnectionState = "disconnected"
	StateConnecting   ConnectionState = "connecting"
	StateConnected    ConnectionState = "connected"
	StateError        ConnectionState = "error"
)

// Notice carries a user-facing notification emitted by the controller.
type Notice struct {
	Seq   uint64 `json:"seq"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// StatusSnapshot is returned by the helper daemon and used by tray/CLI.
type StatusSnapshot struct {
	State          ConnectionState  `json:"state"`
	Mode           string           `json:"mode"`
	CountryCode    string           `json:"country_code,omitempty"`
	CountryName    string           `json:"country_name,omitempty"`
	ActiveServer   *Server          `json:"active_server,omitempty"`
	Candidates     []Server         `json:"candidates,omitempty"`
	LastError      string           `json:"last_error,omitempty"`
	SourceUsed     string           `json:"source_used,omitempty"`
	UsingCache     bool             `json:"using_cache"`
	CacheAge       string           `json:"cache_age,omitempty"`
	Countries      []CountrySummary `json:"countries,omitempty"`
	Notice         *Notice          `json:"notice,omitempty"`
	UpdatedAt      time.Time        `json:"updated_at"`
	PerAppReady    bool             `json:"per_app_ready"`
	HelperRunning  bool             `json:"helper_running"`
}
