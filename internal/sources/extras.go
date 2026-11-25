package sources

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/uri-travoski/MyOVPN/internal/core"
	"github.com/uri-travoski/MyOVPN/internal/ovpn"
)

// FreeOpenVPNSource attempts to fetch FreeOpenVPN.org configs if reachable without bot challenge.
// Disabled by default because FreeOpenVPN.org commonly enforces Cloudflare 403 / CAPTCHA.
type FreeOpenVPNSource struct {
	BaseURL string
}

func NewFreeOpenVPNSource() *FreeOpenVPNSource {
	return &FreeOpenVPNSource{BaseURL: "https://www.freeopenvpn.org"}
}

func (s *FreeOpenVPNSource) ID() string   { return "freeopenvpn" }
func (s *FreeOpenVPNSource) Name() string { return "FreeOpenVPN" }
func (s *FreeOpenVPNSource) Tag() string  { return "FO" }

var freeOpenVPNPassRegex = regexp.MustCompile(`(?i)Password\s*[:=]\s*([a-zA-Z0-9!@#$%^&*_-]+)`)

func (s *FreeOpenVPNSource) Fetch(ctx context.Context, client *http.Client) ([]core.Server, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.BaseURL+"/en/", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MyOVPN/1.1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("freeopenvpn HTTP %d (bot check or unavailable)", resp.StatusCode)
	}
	// If password is text, extract; if image-based or missing, skip cleanly per PRD
	m := freeOpenVPNPassRegex.FindStringSubmatch(string(body))
	if len(m) < 2 {
		return nil, fmt.Errorf("freeopenvpn plaintext password not found (likely image-gated)")
	}
	return nil, nil
}

// VPNObratnoSource fetches optional Russian OpenVPN configs from vpnobratno.info (off by default).
type VPNObratnoSource struct {
	BaseURL string
}

func NewVPNObratnoSource() *VPNObratnoSource {
	return &VPNObratnoSource{BaseURL: "https://vpnobratno.info"}
}

func (s *VPNObratnoSource) ID() string   { return "vpnobratno" }
func (s *VPNObratnoSource) Name() string { return "VPNObratno" }
func (s *VPNObratnoSource) Tag() string  { return "VO" }

var ovpnHrefRegex = regexp.MustCompile(`href=["']([^"']+\.(?:ovpn|zip))["']`)

func (s *VPNObratnoSource) Fetch(ctx context.Context, client *http.Client) ([]core.Server, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.BaseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MyOVPN/1.1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	_ = resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vpnobratno unavailable: %v (HTTP %d)", err, resp.StatusCode)
	}

	var out []core.Server
	for _, match := range ovpnHrefRegex.FindAllStringSubmatch(string(body), 10) {
		target := match[1]
		if !strings.HasPrefix(target, "http") {
			target = strings.TrimRight(s.BaseURL, "/") + "/" + strings.TrimLeft(target, "/")
		}
		ovReq, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			continue
		}
		ovResp, err := client.Do(ovReq)
		if err != nil {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(ovResp.Body, 2<<20))
		_ = ovResp.Body.Close()
		if err != nil || ovResp.StatusCode != http.StatusOK {
			continue
		}
		if strings.HasSuffix(strings.ToLower(target), ".zip") {
			zServers := ParseOVPNZip(raw, s.ID(), s.Tag(), "RU", "Russia", core.AuthUserPass, "vpn", "vpn")
			out = append(out, zServers...)
			continue
		}
		sanitized, err := ovpn.SanitizeAndValidate(string(raw), ovpn.SanitizeOptions{})
		if err != nil {
			continue
		}
		port := sanitized.RemotePort
		var portPtr *int
		if port > 0 {
			p := port
			portPtr = &p
		}
		out = append(out, core.Server{
			ID:           fmt.Sprintf("vpnobratno-%s-%d", sanitized.RemoteHost, port),
			Source:       s.ID(),
			SourceTag:    s.Tag(),
			CountryCode:  "RU",
			CountryName:  "Russia",
			IP:           sanitized.RemoteHost,
			Port:         portPtr,
			Proto:        sanitized.Proto,
			OVPNText:     sanitized.ConfigText,
			Auth:         core.AuthUserPass,
			Username:     "vpn",
			Password:     "vpn",
			StrongCipher: sanitized.StrongCipher,
		})
	}
	return out, nil
}

// CustomURLsSource fetches user-configured URLs returning either VPN Gate CSV or a ZIP of .ovpn files.
type CustomURLsSource struct {
	URLs []string
}

func NewCustomURLsSource(urls []string) *CustomURLsSource {
	return &CustomURLsSource{URLs: urls}
}

func (s *CustomURLsSource) ID() string   { return "custom_url" }
func (s *CustomURLsSource) Name() string { return "Custom URL" }
func (s *CustomURLsSource) Tag() string  { return "CUS" }

func (s *CustomURLsSource) Fetch(ctx context.Context, client *http.Client) ([]core.Server, error) {
	if len(s.URLs) == 0 {
		return nil, nil
	}
	var out []core.Server
	for _, u := range s.URLs {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "MyOVPN/1.1")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			continue
		}

		// Detect ZIP magic bytes "PK\x03\x04"
		if len(data) >= 4 && data[0] == 'P' && data[1] == 'K' {
			out = append(out, ParseOVPNZip(data, s.ID(), s.Tag(), "ZZ", "Custom URL", core.AuthNone, "", "")...)
			continue
		}
		if servers, err := ParseVPNGateCSV(bytes.NewReader(data), s.ID(), s.Tag()); err == nil {
			out = append(out, servers...)
		}
	}
	return out, nil
}

// ParseOVPNZip extracts and sanitizes all .ovpn files inside a ZIP archive.
func ParseOVPNZip(zipBytes []byte, sourceID, sourceTag, defaultCC, defaultCName string, auth core.AuthType, user, pass string) []core.Server {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil
	}
	var out []core.Server
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix(strings.ToLower(f.Name), ".ovpn") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(rc, 512<<10))
		_ = rc.Close()
		if err != nil {
			continue
		}
		sanitized, err := ovpn.SanitizeAndValidate(string(raw), ovpn.SanitizeOptions{})
		if err != nil {
			continue
		}
		cc, cname := inferCountryFromFilename(filepath.Base(f.Name))
		if cc == "ZZ" && defaultCC != "" {
			cc = defaultCC
			cname = defaultCName
		}
		port := sanitized.RemotePort
		var portPtr *int
		if port > 0 {
			p := port
			portPtr = &p
		}
		out = append(out, core.Server{
			ID:           fmt.Sprintf("%s-%s-%s-%d", sourceID, cc, sanitized.RemoteHost, port),
			Source:       sourceID,
			SourceTag:    sourceTag,
			CountryCode:  cc,
			CountryName:  cname,
			IP:           sanitized.RemoteHost,
			Hostname:     sanitized.RemoteHost,
			Port:         portPtr,
			Proto:        sanitized.Proto,
			OVPNText:     sanitized.ConfigText,
			Auth:         auth,
			Username:     user,
			Password:     pass,
			StrongCipher: sanitized.StrongCipher,
		})
	}
	return out
}
