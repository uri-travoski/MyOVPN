package ipc

import "github.com/uri-travoski/MyOVPN/internal/core"

// Command constants define the fixed allowlist of helper actions.
const (
	CmdStatus       = "status"
	CmdRefresh      = "refresh"
	CmdConnect      = "connect"
	CmdNext         = "next"
	CmdDisconnect   = "disconnect"
	CmdLaunch       = "launch"
	CmdCleanup      = "cleanup"
	CmdSetMode      = "set_mode"
	CmdReloadConfig = "reload_config"
	CmdQuit         = "quit"
)

// Request represents a fixed-schema RPC call from tray/CLI to helper daemon.
type Request struct {
	Command     string            `json:"command"`
	CountryCode string            `json:"country_code,omitempty"`
	ServerID    string            `json:"server_id,omitempty"`
	AppID       string            `json:"app_id,omitempty"`
	Mode        string            `json:"mode,omitempty"`
	Force       bool              `json:"force,omitempty"`
	SessionEnv  map[string]string `json:"session_env,omitempty"`
}

// Response represents the helper daemon reply.
type Response struct {
	OK     bool                 `json:"ok"`
	Error  string               `json:"error,omitempty"`
	Status *core.StatusSnapshot `json:"status,omitempty"`
}

// CallerInfo identifies the authenticated local IPC peer.
type CallerInfo struct {
	UID uint32
	GID uint32
}

// FilterSessionEnv copies only safe GUI session environment keys from the caller.
func FilterSessionEnv(in map[string]string) []string {
	allowed := []string{
		"DISPLAY",
		"WAYLAND_DISPLAY",
		"XAUTHORITY",
		"DBUS_SESSION_BUS_ADDRESS",
		"XDG_RUNTIME_DIR",
		"HOME",
		"USER",
		"LANG",
		"PATH",
	}
	var out []string
	for _, k := range allowed {
		if v, ok := in[k]; ok && v != "" {
			out = append(out, k+"="+v)
		}
	}
	return out
}
