package assets

import (
	_ "embed"
	"runtime"
)

//go:embed icons/tray-white.png
var trayWhitePNG []byte

//go:embed icons/tray-green.png
var trayGreenPNG []byte

//go:embed icons/tray-red.png
var trayRedPNG []byte

//go:embed icons/tray-white.ico
var trayWhiteICO []byte

//go:embed icons/tray-green.ico
var trayGreenICO []byte

//go:embed icons/tray-red.ico
var trayRedICO []byte

// TrayStateIcon returns the embedded icon bytes for the current OS (ICO on Windows, PNG elsewhere).
func TrayStateIcon(state string) []byte {
	isWin := runtime.GOOS == "windows"
	switch state {
	case "green":
		if isWin {
			return trayGreenICO
		}
		return trayGreenPNG
	case "red":
		if isWin {
			return trayRedICO
		}
		return trayRedPNG
	default:
		if isWin {
			return trayWhiteICO
		}
		return trayWhitePNG
	}
}
