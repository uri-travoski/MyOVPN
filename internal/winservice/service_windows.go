//go:build windows

package winservice

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	ServiceName        = "MyOVPNHelper"
	ServiceDisplayName = "MyOVPN Helper Service"
	ServiceDesc        = "Manages OpenVPN tunnels for MyOVPN"
)

// IsServiceProcess returns true if running under the Windows Service Control Manager.
func IsServiceProcess() bool {
	inService, err := svc.IsWindowsService()
	return err == nil && inService
}

// Run executes the Windows Service lifecycle loop.
func Run(runDaemon func(ctx context.Context) error) error {
	return svc.Run(ServiceName, &handler{runDaemon: runDaemon})
}

type handler struct {
	runDaemon func(ctx context.Context) error
}

func (h *handler) Execute(_ []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- h.runDaemon(ctx)
	}()

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				<-errCh
				return false, 0
			}
		case <-errCh:
			return false, 1
		}
	}
}

// InstallAndStart registers and starts MyOVPNHelper as an auto-start LocalSystem service.
func InstallAndStart(exePath string) error {
	if exePath == "" {
		var err error
		exePath, err = os.Executable()
		if err != nil {
			return err
		}
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err == nil {
		_ = s.Close()
		_ = Uninstall()
	}

	s, err = m.CreateService(ServiceName, exePath, mgr.Config{
		StartType:   mgr.StartAutomatic,
		DisplayName: ServiceDisplayName,
		Description: ServiceDesc,
	}, "daemon")
	if err != nil {
		return err
	}
	defer s.Close()

	return s.Start()
}

// Uninstall stops and deletes the MyOVPNHelper Windows service.
func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return nil
	}
	defer s.Close()

	_, _ = s.Control(svc.Stop)
	return s.Delete()
}

// ElevateAndRun invokes `<exe> <args>` with UAC `runas` prompt.
func ElevateAndRun(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verbPtr, _ := syscall.UTF16PtrFromString("runas")
	exePtr, _ := syscall.UTF16PtrFromString(exe)
	argStr := strings.Join(args, " ")
	argPtr, _ := syscall.UTF16PtrFromString(argStr)
	dirPtr, _ := syscall.UTF16PtrFromString(filepath.Dir(exe))

	ret, _, callErr := shellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(exePtr)),
		uintptr(unsafe.Pointer(argPtr)),
		uintptr(unsafe.Pointer(dirPtr)),
		windows.SW_HIDE,
	)
	if ret <= 32 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return callErr
		}
		return fmt.Errorf("UAC elevation failed (code %d)", ret)
	}
	return nil
}

// IsInstalled checks if the MyOVPNHelper service is currently registered.
func IsInstalled() bool {
	cmd := exec.Command("sc.exe", "query", ServiceName)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run() == nil
}

var (
	modshell32    = syscall.NewLazyDLL("shell32.dll")
	shellExecuteW = modshell32.NewProc("ShellExecuteW")
)
