//go:build linux

package netns

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/uri-travoski/MyOVPN/internal/config"
	"github.com/uri-travoski/MyOVPN/internal/core"
	"github.com/uri-travoski/MyOVPN/internal/ovpn"
)

const (
	HostVeth   = "veth-myovpn-h"
	NetnsVeth  = "veth-myovpn-n"
	HostIP     = "10.200.200.1"
	NetnsIP    = "10.200.200.2"
	SubnetCIDR = "10.200.200.0/24"
	NetnsDir   = "/etc/netns/" + config.NetnsName
	ResolvPath = NetnsDir + "/resolv.conf"
)

var dhcpDNSRegex = regexp.MustCompile(`dhcp-option\s+DNS\s+([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)`)

// Runner manages the `myovpn` network namespace, kill switch, and OpenVPN process.
type Runner struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	workDir  string
	authFile string
	ovpnFile string
	tunUp    bool
	lastErr  string
	onLog    func(string)
}

func NewRunner(onLog func(string)) *Runner {
	return &Runner{onLog: onLog}
}

func (r *Runner) logf(format string, args ...any) {
	if r.onLog != nil {
		r.onLog(fmt.Sprintf(format, args...))
	}
}

// Exists checks whether the `myovpn` network namespace currently exists.
func Exists() bool {
	_, err := os.Stat("/run/netns/" + config.NetnsName)
	return err == nil
}

// EnsureNamespace idempotently creates the network namespace, veth pair, host NAT,
// IPv6 block, DNS resolv.conf, and strict nftables kill switch allowing only (serverIP, serverPort).
func EnsureNamespace(ctx context.Context, serverIP string, serverPort int, proto string) error {
	if net.ParseIP(serverIP) == nil {
		return fmt.Errorf("invalid resolved server IP %q", serverIP)
	}
	if serverPort <= 0 || serverPort > 65535 {
		serverPort = 1194
	}
	proto = strings.ToLower(strings.TrimSpace(proto))
	if proto != "tcp" {
		proto = "udp"
	}

	if err := os.MkdirAll(NetnsDir, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(ResolvPath); os.IsNotExist(err) {
		if err := WriteResolvConf([]string{"1.1.1.1", "9.9.9.9"}); err != nil {
			return err
		}
	}

	// Create netns if missing
	if !Exists() {
		if out, err := runCmd(ctx, "ip", "netns", "add", config.NetnsName); err != nil {
			return fmt.Errorf("ip netns add: %v (%s)", err, out)
		}
	}

	// Ensure loopback is up inside namespace
	_, _ = runCmd(ctx, "ip", "-n", config.NetnsName, "link", "set", "lo", "up")

	// Disable IPv6 inside namespace to prevent IPv6 leaks
	_, _ = runCmd(ctx, "ip", "netns", "exec", config.NetnsName, "sysctl", "-q", "-w", "net.ipv6.conf.all.disable_ipv6=1")
	_, _ = runCmd(ctx, "ip", "netns", "exec", config.NetnsName, "sysctl", "-q", "-w", "net.ipv6.conf.default.disable_ipv6=1")

	// Recreate veth pair if either side is missing
	if _, err := runCmd(ctx, "ip", "link", "show", HostVeth); err != nil {
		_, _ = runCmd(ctx, "ip", "-n", config.NetnsName, "link", "del", NetnsVeth)
		if out, err := runCmd(ctx, "ip", "link", "add", HostVeth, "type", "veth", "peer", "name", NetnsVeth, "netns", config.NetnsName); err != nil {
			return fmt.Errorf("create veth pair: %v (%s)", err, out)
		}
	}

	// Configure host side
	_, _ = runCmd(ctx, "ip", "addr", "replace", HostIP+"/24", "dev", HostVeth)
	if out, err := runCmd(ctx, "ip", "link", "set", HostVeth, "up"); err != nil {
		return fmt.Errorf("host veth up: %v (%s)", err, out)
	}

	// Configure namespace side
	_, _ = runCmd(ctx, "ip", "-n", config.NetnsName, "addr", "replace", NetnsIP+"/24", "dev", NetnsVeth)
	if out, err := runCmd(ctx, "ip", "-n", config.NetnsName, "link", "set", NetnsVeth, "up"); err != nil {
		return fmt.Errorf("netns veth up: %v (%s)", err, out)
	}

	// Only a /32 route to the VPN server IP via host veth (NO default route on veth!)
	_, _ = runCmd(ctx, "ip", "-n", config.NetnsName, "route", "flush", "table", "main", "dev", NetnsVeth)
	_, _ = runCmd(ctx, "ip", "-n", config.NetnsName, "route", "replace", "10.200.200.0/24", "dev", NetnsVeth)
	if out, err := runCmd(ctx, "ip", "-n", config.NetnsName, "route", "replace", serverIP+"/32", "via", HostIP, "dev", NetnsVeth); err != nil {
		return fmt.Errorf("netns server route: %v (%s)", err, out)
	}

	// Host IP forwarding + NAT + Docker-compatible FORWARD allow
	_, _ = runCmd(ctx, "sysctl", "-q", "-w", "net.ipv4.ip_forward=1")
	hostNFT := fmt.Sprintf(`
table ip myovpn_host
delete table ip myovpn_host
table ip myovpn_host {
	chain forward {
		type filter hook forward priority -5; policy accept;
		iifname "%s" accept
		oifname "%s" ct state established,related accept
	}
	chain postrouting {
		type nat hook postrouting priority srcnat; policy accept;
		ip saddr %s oifname != "%s" masquerade
	}
}
`, HostVeth, HostVeth, SubnetCIDR, HostVeth)
	if out, err := runCmdStdin(ctx, hostNFT, "nft", "-f", "-"); err != nil {
		return fmt.Errorf("host nftables: %v (%s)", err, out)
	}

	// Ensure iptables FORWARD does not drop packets when Docker sets FORWARD policy DROP
	if _, err := exec.LookPath("iptables"); err == nil {
		if _, err := runCmd(ctx, "iptables", "-C", "FORWARD", "-i", HostVeth, "-j", "ACCEPT"); err != nil {
			_, _ = runCmd(ctx, "iptables", "-I", "FORWARD", "1", "-i", HostVeth, "-j", "ACCEPT")
		}
		if _, err := runCmd(ctx, "iptables", "-C", "FORWARD", "-o", HostVeth, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"); err != nil {
			_, _ = runCmd(ctx, "iptables", "-I", "FORWARD", "1", "-o", HostVeth, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT")
		}
	}

	// Strict kill-switch nftables inside namespace
	nsNFT := fmt.Sprintf(`
table inet myovpn_ns
delete table inet myovpn_ns
table inet myovpn_ns {
	chain output {
		type filter hook output priority 0; policy drop;
		oifname "lo" accept
		oifname "tun0" accept
		oifname "%s" ip daddr %s %s dport %d accept
	}
	chain input {
		type filter hook input priority 0; policy drop;
		iifname "lo" accept
		iifname "tun0" accept
		iifname "%s" ct state established,related accept
	}
}
`, NetnsVeth, serverIP, proto, serverPort, NetnsVeth)
	if out, err := runCmdStdin(ctx, nsNFT, "ip", "netns", "exec", config.NetnsName, "nft", "-f", "-"); err != nil {
		return fmt.Errorf("netns nftables: %v (%s)", err, out)
	}

	return nil
}

// WriteResolvConf updates /etc/netns/myovpn/resolv.conf atomically.
func WriteResolvConf(dnsServers []string) error {
	if len(dnsServers) == 0 {
		dnsServers = []string{"1.1.1.1"}
	}
	if err := os.MkdirAll(NetnsDir, 0o755); err != nil {
		return err
	}
	var sb strings.Builder
	for _, d := range dnsServers {
		d = strings.TrimSpace(d)
		if net.ParseIP(d) != nil {
			sb.WriteString("nameserver ")
			sb.WriteString(d)
			sb.WriteByte('\n')
		}
	}
	if sb.Len() == 0 {
		sb.WriteString("nameserver 1.1.1.1\n")
	}
	// Write in-place so existing processes with bind-mounted /etc/resolv.conf see the update immediately.
	return os.WriteFile(ResolvPath, []byte(sb.String()), 0o644)
}

// StartOpenVPN stops any previous OpenVPN inside the namespace (keeping Chrome/apps alive),
// updates the namespace firewall for srv, and starts OpenVPN inside `myovpn`.
func (r *Runner) StartOpenVPN(ctx context.Context, srv core.Server, connectTimeout time.Duration) error {
	r.StopOpenVPN()

	resolvedIP := strings.TrimSpace(srv.IP)
	if net.ParseIP(resolvedIP) == nil && srv.Hostname != "" {
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", srv.Hostname)
		if err != nil || len(ips) == 0 {
			return fmt.Errorf("failed to resolve %q: %v", srv.Hostname, err)
		}
		resolvedIP = ips[0].String()
	}
	port := 1194
	if srv.Port != nil && *srv.Port > 0 {
		port = *srv.Port
	}

	if err := EnsureNamespace(ctx, resolvedIP, port, srv.Proto); err != nil {
		return err
	}
	_ = WriteResolvConf([]string{"1.1.1.1"})

	workDir, err := os.MkdirTemp("", "myovpn-run-*")
	if err != nil {
		return err
	}
	_ = os.Chmod(workDir, 0o700)

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
		ResolvedIP:       resolvedIP,
		AuthUserPassFile: authFile,
		ForceTunName:     "tun0",
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

	cmd := exec.Command("ip", "netns", "exec", config.NetnsName,
		"openvpn", "--config", ovpnFile, "--redirect-gateway", "def1", "--verb", "3")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = os.RemoveAll(workDir)
		return err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(workDir)
		return fmt.Errorf("start openvpn: %w", err)
	}

	r.mu.Lock()
	r.cmd = cmd
	r.workDir = workDir
	r.authFile = authFile
	r.ovpnFile = ovpnFile
	r.tunUp = false
	r.lastErr = ""
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
	sentResult := false
	var pushedDNS []string

	for scanner.Scan() {
		line := scanner.Text()
		r.logf("openvpn: %s", line)

		if strings.Contains(line, "PUSH_REPLY") {
			for _, m := range dhcpDNSRegex.FindAllStringSubmatch(line, -1) {
				pushedDNS = append(pushedDNS, m[1])
			}
			if len(pushedDNS) > 0 {
				_ = WriteResolvConf(pushedDNS)
			}
		}

		if !sentResult && strings.Contains(line, "Initialization Sequence Completed") {
			r.mu.Lock()
			r.tunUp = true
			r.mu.Unlock()
			sentResult = true
			connectedCh <- nil
		}
	}

	err := cmd.Wait()
	r.mu.Lock()
	r.tunUp = false
	if err != nil {
		r.lastErr = err.Error()
	}
	r.mu.Unlock()

	if !sentResult {
		if err == nil {
			err = errors.New("openvpn exited before connecting")
		}
		connectedCh <- err
	}
}

// StopOpenVPN terminates only the OpenVPN process inside the namespace and removes mode-600 auth files.
func (r *Runner) StopOpenVPN() {
	r.mu.Lock()
	cmd := r.cmd
	workDir := r.workDir
	r.cmd = nil
	r.workDir = ""
	r.authFile = ""
	r.ovpnFile = ""
	r.tunUp = false
	r.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() {
			_, _ = cmd.Process.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}
	if workDir != "" {
		_ = os.RemoveAll(workDir)
	}
}

// Cleanup tears down OpenVPN, the `myovpn` namespace, veth interfaces, nftables rules, and resolv.conf dir.
func (r *Runner) Cleanup(ctx context.Context) error {
	r.StopOpenVPN()
	return CleanupAll(ctx)
}

// CleanupAll idempotently removes all per-app namespace, veth, and nftables artifacts.
func CleanupAll(ctx context.Context) error {
	_, _ = runCmd(ctx, "nft", "delete", "table", "ip", "myovpn_host")
	if _, err := exec.LookPath("iptables"); err == nil {
		_, _ = runCmd(ctx, "iptables", "-D", "FORWARD", "-i", HostVeth, "-j", "ACCEPT")
		_, _ = runCmd(ctx, "iptables", "-D", "FORWARD", "-o", HostVeth, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT")
	}
	_, _ = runCmd(ctx, "ip", "link", "del", HostVeth)
	if Exists() {
		_, _ = runCmd(ctx, "ip", "netns", "exec", config.NetnsName, "nft", "delete", "table", "inet", "myovpn_ns")
		_, _ = runCmd(ctx, "ip", "netns", "del", config.NetnsName)
	}
	_ = os.RemoveAll(NetnsDir)
	return nil
}

// LaunchAppAsUser starts a configured user application inside the `myovpn` namespace
// with privileges dropped to uid/gid (never running the app as root).
func LaunchAppAsUser(uid, gid uint32, env []string, app config.AppEntry) error {
	if !Exists() {
		// Create locked-down namespace with kill switch active even before first connect
		if err := EnsureNamespace(context.Background(), "127.0.0.1", 1194, "udp"); err != nil {
			return err
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	// Ensure Chrome profile directory exists and belongs to the user with distinct VPN theme color
	if app.ID == "chrome" {
		for _, a := range app.Args {
			if strings.HasPrefix(a, "--user-data-dir=") {
				profDir := strings.TrimPrefix(a, "--user-data-dir=")
				_ = ensureChromeVPNProfile(profDir, int(uid), int(gid))
			}
		}
	}

	args := []string{
		"netns", "exec", config.NetnsName,
		exe, "_netns-launch",
		"--uid", strconv.FormatUint(uint64(uid), 10),
		"--gid", strconv.FormatUint(uint64(gid), 10),
		"--cmd", app.Command,
		"--",
	}
	args = append(args, app.Args...)

	cmd := exec.Command("ip", args...)
	cmd.Env = env
	// Detach so closing daemon doesn't kill user's browser unexpectedly unless quit
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func ensureChromeVPNProfile(profDir string, uid, gid int) error {
	defaultDir := filepath.Join(profDir, "Default")
	if err := os.MkdirAll(defaultDir, 0o700); err != nil {
		return err
	}
	prefsPath := filepath.Join(defaultDir, "Preferences")
	if _, err := os.Stat(prefsPath); os.IsNotExist(err) {
		// Set a distinct teal/green-tinted profile theme so VPN Chrome stands out visually
		prefs := `{"profile":{"name":"MyOVPN (VPN)"},"autogenerated":{"theme":{"color":-14513374}}}`
		_ = os.WriteFile(prefsPath, []byte(prefs), 0o600)
	}
	_ = chownRecursive(profDir, uid, gid)
	return nil
}

func chownRecursive(root string, uid, gid int) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		return os.Chown(path, uid, gid)
	})
}

func runCmd(ctx context.Context, name string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, name, args...)
	b, err := c.CombinedOutput()
	return strings.TrimSpace(string(b)), err
}

func runCmdStdin(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, name, args...)
	c.Stdin = strings.NewReader(stdin)
	b, err := c.CombinedOutput()
	return strings.TrimSpace(string(b)), err
}
