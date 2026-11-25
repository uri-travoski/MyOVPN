//go:build linux

package ipc

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

const SocketPath = "/run/myovpn.sock"

// Listen creates the Unix socket at SocketPath with group `myovpn` permissions if available.
func Listen() (net.Listener, error) {
	_ = os.Remove(SocketPath)
	ln, err := net.Listen("unix", SocketPath)
	if err != nil {
		return nil, err
	}
	// Allow local desktop users (authenticated via SO_PEERCRED in PeerCaller) to connect
	if grp, err := user.LookupGroup("myovpn"); err == nil {
		if gid, err := strconv.Atoi(grp.Gid); err == nil {
			_ = os.Chown(SocketPath, 0, gid)
		}
	}
	_ = os.Chmod(SocketPath, 0o666)
	return ln, nil
}

// Dial connects to the local MyOVPN helper Unix socket.
func Dial() (net.Conn, error) {
	return net.Dial("unix", SocketPath)
}

// PeerCaller verifies the connecting local process via SO_PEERCRED.
func PeerCaller(conn net.Conn) (CallerInfo, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return CallerInfo{}, fmt.Errorf("expected unix socket connection")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return CallerInfo{}, err
	}
	var ucred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		ucred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return CallerInfo{}, err
	}
	if credErr != nil {
		return CallerInfo{}, credErr
	}
	if ucred.Uid != 0 && ucred.Uid < 1000 {
		return CallerInfo{}, fmt.Errorf("unauthorized uid %d", ucred.Uid)
	}
	return CallerInfo{UID: ucred.Uid, GID: ucred.Gid}, nil
}
