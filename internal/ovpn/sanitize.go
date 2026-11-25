package ovpn

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// DangerousDirectives lists OpenVPN directives capable of executing scripts or loading native plugins.
var DangerousDirectives = map[string]bool{
	"up":                    true,
	"down":                  true,
	"script-security":       true,
	"plugin":                true,
	"route-up":              true,
	"route-pre-down":        true,
	"ipchange":              true,
	"tls-verify":            true,
	"client-connect":        true,
	"client-disconnect":     true,
	"learn-address":         true,
	"auth-user-pass-verify": true,
	"tls-crypt-v2-verify":   true,
	"iproute":               true,
	"management":            true,
}

const ModernDataCiphers = "AES-256-GCM:AES-128-GCM:CHACHA20-POLY1305:AES-128-CBC"

// SanitizeOptions configures config sanitization and runtime injection.
type SanitizeOptions struct {
	ExpectedIP       string
	ExpectedHostname string
	ResolvedIP       string // Optional: if set, replaces remote host with validated IP (for netns without DNS)
	AuthUserPassFile string // Optional: path to mode-600 user/pass file
	ForceTunName     string // Optional: e.g., "tun0" so per-app nftables matches consistently
}

// SanitizeResult holds the sanitized .ovpn text and extracted metadata.
type SanitizeResult struct {
	ConfigText   string
	RemoteHost   string
	RemotePort   int
	Proto        string
	StrongCipher bool
}

// SanitizeAndValidate strips dangerous script/plugin directives, validates `remote`,
// enforces OpenVPN 2.6 `data-ciphers` compatibility (FR-9), and never weakens TLS checks.
func SanitizeAndValidate(raw string, opts SanitizeOptions) (SanitizeResult, error) {
	var res SanitizeResult
	if strings.TrimSpace(raw) == "" {
		return res, errors.New("empty ovpn config")
	}

	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var out []string
	var hasDataCiphers bool
	var cipherVal string
	var dataCiphersVal string
	inBlock := false
	matchedRemote := false

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			out = append(out, line)
			continue
		}

		// Preserve inline XML blocks (<ca>, <cert>, <key>, <tls-auth>, <tls-crypt>) verbatim
		if strings.HasPrefix(trimmed, "<") && !strings.HasPrefix(trimmed, "</") {
			inBlock = true
			out = append(out, line)
			continue
		}
		if strings.HasPrefix(trimmed, "</") {
			inBlock = false
			out = append(out, line)
			continue
		}
		if inBlock {
			out = append(out, line)
			continue
		}

		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}
		directive := strings.ToLower(fields[0])

		// Security: strip any directive that can run external scripts/plugins or hijack management
		if DangerousDirectives[directive] {
			continue
		}

		switch directive {
		case "remote":
			if len(fields) < 2 {
				return res, errors.New("invalid remote directive")
			}
			host := fields[1]
			port := 1194
			if len(fields) >= 3 {
				if p, err := strconv.Atoi(fields[2]); err == nil && p > 0 && p <= 65535 {
					port = p
				}
			}
			if !remoteMatchesExpected(host, opts.ExpectedIP, opts.ExpectedHostname) {
				return res, fmt.Errorf("remote host %q does not match expected %q / %q", host, opts.ExpectedIP, opts.ExpectedHostname)
			}
			matchedRemote = true
			res.RemoteHost = host
			res.RemotePort = port
			if len(fields) >= 4 {
				res.Proto = normalizeProto(fields[3])
			}
			if opts.ResolvedIP != "" {
				if len(fields) >= 4 {
					line = fmt.Sprintf("remote %s %d %s", opts.ResolvedIP, port, fields[3])
				} else {
					line = fmt.Sprintf("remote %s %d", opts.ResolvedIP, port)
				}
			}
		case "proto":
			if len(fields) >= 2 && res.Proto == "" {
				res.Proto = normalizeProto(fields[1])
			}
		case "dev":
			if opts.ForceTunName != "" && len(fields) >= 2 && strings.HasPrefix(strings.ToLower(fields[1]), "tun") {
				line = "dev " + opts.ForceTunName
			}
		case "cipher":
			if len(fields) >= 2 {
				cipherVal = strings.ToUpper(fields[1])
			}
		case "data-ciphers":
			hasDataCiphers = true
			if len(fields) >= 2 {
				dataCiphersVal = strings.ToUpper(fields[1])
			}
			// FR-9: ensure OpenVPN 2.6 accepts AES-128-CBC fallback if server uses it
			if !strings.Contains(dataCiphersVal, "AES-128-CBC") && strings.Contains(cipherVal, "AES-128-CBC") {
				line = "data-ciphers " + ModernDataCiphers
			} else if strings.Contains(dataCiphersVal, "AES-128-CBC") && !strings.Contains(dataCiphersVal, "AES-256-GCM") {
				line = "data-ciphers " + ModernDataCiphers
			}
		case "auth-user-pass":
			if opts.AuthUserPassFile != "" {
				line = "auth-user-pass " + opts.AuthUserPassFile
			}
		}

		out = append(out, line)
	}
	if err := scanner.Err(); err != nil {
		return res, err
	}

	if !matchedRemote {
		return res, errors.New("ovpn config missing valid remote directive")
	}

	if !hasDataCiphers {
		out = append(out, "data-ciphers "+ModernDataCiphers)
	}

	if opts.AuthUserPassFile != "" {
		hasAuthDirective := false
		for _, l := range out {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(l)), "auth-user-pass") {
				hasAuthDirective = true
				break
			}
		}
		if !hasAuthDirective {
			out = append(out, "auth-user-pass "+opts.AuthUserPassFile)
		}
	}

	// Verify no dangerous directive survived
	for _, l := range out {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";") || strings.HasPrefix(t, "<") {
			continue
		}
		f := strings.Fields(t)
		if len(f) > 0 && DangerousDirectives[strings.ToLower(f[0])] {
			return res, fmt.Errorf("dangerous directive %q remained after sanitization", f[0])
		}
	}

	if res.Proto == "" {
		res.Proto = "udp"
	}
	res.StrongCipher = isStrongCipher(cipherVal, dataCiphersVal)
	res.ConfigText = strings.Join(out, "\n") + "\n"
	return res, nil
}

func remoteMatchesExpected(remoteHost, expectedIP, expectedHost string) bool {
	rh := strings.TrimSpace(remoteHost)
	ei := strings.TrimSpace(expectedIP)
	eh := strings.TrimSpace(expectedHost)
	if ei == "" && eh == "" {
		return true
	}
	if ei != "" && strings.EqualFold(rh, ei) {
		return true
	}
	if eh != "" && strings.EqualFold(rh, eh) {
		return true
	}
	// Allow if expectedIP is unset or remoteHost resolves/is identical IP
	if net.ParseIP(rh) == nil && eh != "" && strings.EqualFold(rh, eh) {
		return true
	}
	return false
}

func normalizeProto(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	if strings.HasPrefix(p, "tcp") {
		return "tcp"
	}
	return "udp"
}

func isStrongCipher(cipherVal, dataCiphersVal string) bool {
	check := dataCiphersVal
	if check == "" {
		check = cipherVal
	}
	if check == "" {
		return false
	}
	// If only CBC / BF-CBC is listed without GCM or ChaCha20, consider legacy/weak
	hasModern := strings.Contains(check, "AES-256-GCM") ||
		strings.Contains(check, "AES-128-GCM") ||
		strings.Contains(check, "CHACHA20-POLY1305")
	if !hasModern {
		return false
	}
	// If AES-128-CBC is the explicit `cipher` and data-ciphers only matches CBC, it's weak
	if strings.Contains(cipherVal, "CBC") && !strings.Contains(dataCiphersVal, "GCM") {
		return false
	}
	return true
}
