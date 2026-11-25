//go:build windows

package ipc

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

const PipePath = `\\.\pipe\MyOVPNHelper`

// Listen creates the Windows named pipe allowing LocalSystem, Administrators, and Interactive Users.
func Listen() (net.Listener, error) {
	cfg := &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;IU)",
		MessageMode:        false,
		InputBufferSize:    65536,
		OutputBufferSize:   65536,
	}
	return winio.ListenPipe(PipePath, cfg)
}

// Dial connects to the MyOVPN Windows helper named pipe.
func Dial() (net.Conn, error) {
	t := 2 * time.Second
	return winio.DialPipe(PipePath, &t)
}

// PeerCaller returns default caller info on Windows (access controlled by pipe DACL).
func PeerCaller(_ net.Conn) (CallerInfo, error) {
	return CallerInfo{}, nil
}
