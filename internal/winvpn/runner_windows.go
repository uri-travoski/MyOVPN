//go:build windows

package winvpn

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uri-travoski/MyOVPN/internal/core"
	"github.com/uri-travoski/MyOVPN/internal/ovpn"
)

// Runner manages the bundled openvpn.exe process on Windows (system-wide mode).
type Runner struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	workDir string
	onLog   func(string)
}

func NewRunner(onLog func(string)) *Runner {
	return &Runner{onLog: onLog}
}

func (r *Runner) logf(format string, args ...any) {
	if r.onLog != nil {
		r.onLog(fmt.Sprintf(format, args...))
	}
}

// FindOpenVPN locates bundled openvpn.exe next to the executable or in Program Files.
func FindOpenVPN() (string, error) {
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)
	candidates := []string{
		filepath.Join(exeDir, "openvpn", "openvpn.exe"),
		filepath.Join(exeDir, "openvpn.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "MyOVPN", "openvpn", "openvpn.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "OpenVPN", "bin", "openvpn.exe"),
	}
	for _, c := range candidates {
		if c != "" {
			if _, err := os.Stat(c); err == nil {
				return c, nil
			}
		}
	}
	if p, err := exec.LookPath("openvpn.exe"); err == nil {
		return p, nil
	}
	return "", errors.New("openvpn.exe / VPN driver missing")
}

// StartOpenVPN launches openvpn.exe for srv and waits up to connectTimeout for Initialization Sequence Completed.
func (r *Runner) StartOpenVPN(ctx context.Context, srv core.Server, connectTimeout time.Duration) error {
	r.StopOpenVPN()

	ovpnExe, err := FindOpenVPN()
	if err != nil {
		return err
	}

	workDir, err := os.MkdirTemp("", "myovpn-win-*")
	if err != nil {
		return err
	}

	var authFile string
	if srv.Auth == core.AuthUserPass {
		authFile = filepath.Join(workDir, "auth.txt")
		creds := srv.Username + "\n" + srv.Password + "\n"
		if err := os.WriteFile(authFile, []byte(creds), 0o600); err != nil {
			_ = os.RemoveAll(workDir)
			return err
		}
	}

	sanitized, err := ovpn.SanitizeAndValidate(srv.OVPNText, ovpn.SanitizeOptions{
		ExpectedIP:       srv.IP,
		ExpectedHostname: srv.Hostname,
		AuthUserPassFile: authFile,
	})
	if err != nil {
		_ = os.RemoveAll(workDir)
		return err
	}

	ovpnFile := filepath.Join(workDir, "client.ovpn")
	if err := os.WriteFile(ovpnFile, []byte(sanitized.ConfigText), 0o600); err != nil {
		_ = os.RemoveAll(workDir)
		return err
	}

	args := []string{"--config", ovpnFile, "--windows-driver", "wintun", "--verb", "3"}
	cmd := exec.Command(ovpnExe, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = os.RemoveAll(workDir)
		return err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(workDir)
		return fmt.Errorf("start openvpn.exe: %w", err)
	}

	r.mu.Lock()
	r.cmd = cmd
	r.workDir = workDir
	r.mu.Unlock()

	connectedCh := make(chan error, 1)
	go r.watchOutput(cmd, stdout, connectedCh)

	timer := time.NewTimer(connectTimeout)
	defer timer.Stop()

	select {
	case err := <-connectedCh:
		if err != nil {
			r.StopOpenVPN()
			return err
		}
		return nil
	case <-timer.C:
		r.StopOpenVPN()
		return fmt.Errorf("openvpn connect timed out after %s", connectTimeout)
	case <-ctx.Done():
		r.StopOpenVPN()
		return ctx.Err()
	}
}

func (r *Runner) watchOutput(cmd *exec.Cmd, rc io.ReadCloser, connectedCh chan<- error) {
	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 0, 16*1024), 256*1024)
	sent := false
	for scanner.Scan() {
		line := scanner.Text()
		r.logf("openvpn: %s", line)
		if !sent && strings.Contains(line, "Initialization Sequence Completed") {
			sent = true
			connectedCh <- nil
		}
	}
	err := cmd.Wait()
	if !sent {
		if err == nil {
			err = errors.New("openvpn.exe exited before connecting")
		}
		connectedCh <- err
	}
}

// StopOpenVPN terminates openvpn.exe and removes temporary auth/config files.
func (r *Runner) StopOpenVPN() {
	r.mu.Lock()
	cmd := r.cmd
	workDir := r.workDir
	r.cmd = nil
	r.workDir = ""
	r.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
	if workDir != "" {
		_ = os.RemoveAll(workDir)
	}
}
