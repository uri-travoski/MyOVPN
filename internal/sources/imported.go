package sources

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/uri-travoski/MyOVPN/internal/core"
	"github.com/uri-travoski/MyOVPN/internal/ovpn"
)

// ImportedSource loads user-supplied .ovpn configs (e.g. Proton VPN Free "Trusted fallback" or own VPS).
type ImportedSource struct {
	Dir string
}

func NewImportedSource(dir string) *ImportedSource {
	return &ImportedSource{Dir: dir}
}

func (s *ImportedSource) ID() string   { return "imported" }
func (s *ImportedSource) Name() string { return "Imported" }
func (s *ImportedSource) Tag() string  { return "IMP" }

func (s *ImportedSource) Fetch(_ context.Context, _ *http.Client) ([]core.Server, error) {
	if strings.TrimSpace(s.Dir) == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []core.Server
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".ovpn") {
			continue
		}
		fullPath := filepath.Join(s.Dir, entry.Name())
		raw, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		sanitized, err := ovpn.SanitizeAndValidate(string(raw), ovpn.SanitizeOptions{})
		if err != nil {
			continue
		}
		cc, cname := inferCountryFromFilename(entry.Name())
		auth := core.AuthNone
		if strings.Contains(strings.ToLower(sanitized.ConfigText), "auth-user-pass") {
			auth = core.AuthUserPass
		}
		port := sanitized.RemotePort
		var portPtr *int
		if port > 0 {
			p := port
			portPtr = &p
		}
		// Give imported trusted configs a high synthetic speed so they appear cleanly if enabled
		speed := int64(100_000_000)
		ping := 30
		out = append(out, core.Server{
			ID:           fmt.Sprintf("imported-%s", strings.TrimSuffix(entry.Name(), ".ovpn")),
			Source:       s.ID(),
			SourceTag:    s.Tag(),
			CountryCode:  cc,
			CountryName:  cname,
			IP:           sanitized.RemoteHost,
			Hostname:     sanitized.RemoteHost,
			Port:         portPtr,
			Proto:        sanitized.Proto,
			SpeedBps:     &speed,
			PingMs:       &ping,
			OVPNText:     sanitized.ConfigText,
			Auth:         auth,
			StrongCipher: sanitized.StrongCipher,
		})
	}
	return out, nil
}

func inferCountryFromFilename(name string) (string, string) {
	upper := strings.ToUpper(name)
	if strings.Contains(upper, "PROTON") {
		return "ZZ", "Trusted Fallback (Proton)"
	}
	if len(upper) >= 2 && upper[0] >= 'A' && upper[0] <= 'Z' && upper[1] >= 'A' && upper[1] <= 'Z' {
		if len(upper) == 2 || upper[2] == '-' || upper[2] == '_' || upper[2] == '.' {
			cc := upper[:2]
			return cc, countryNameFromCode(cc)
		}
	}
	return "ZZ", "Imported / Custom"
}
