package sources

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/uri-travoski/MyOVPN/internal/core"
	"github.com/uri-travoski/MyOVPN/internal/ovpn"
)

const DefaultVPNGateURL = "https://www.vpngate.net/api/iphone/"

// DefaultVPNGateMirrors are the four PHP proxy mirrors referenced in the PRD (ovfinder).
var DefaultVPNGateMirrors = []string{
	"https://helloapp.site/vpngate/",
	"https://m.amv.org.mx/vpngate/",
	"https://proxy.tfdracing.nl/vpngate/",
	"https://nakabel.com/vpngate/",
}

// VPNGateSource fetches from the official VPN Gate CSV endpoint.
type VPNGateSource struct {
	URL string
}

func NewVPNGateSource() *VPNGateSource {
	return &VPNGateSource{URL: DefaultVPNGateURL}
}

func (s *VPNGateSource) ID() string   { return "vpngate" }
func (s *VPNGateSource) Name() string { return "VPN Gate" }
func (s *VPNGateSource) Tag() string  { return "VG" }

func (s *VPNGateSource) Fetch(ctx context.Context, client *http.Client) ([]core.Server, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MyOVPN/1.1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vpngate HTTP %d", resp.StatusCode)
	}
	return ParseVPNGateCSV(resp.Body, s.ID(), s.Tag())
}

// VPNGateMirrorsSource tries third-party proxy mirrors when VPN Gate is unreachable.
type VPNGateMirrorsSource struct {
	Mirrors []string
}

func NewVPNGateMirrorsSource() *VPNGateMirrorsSource {
	return &VPNGateMirrorsSource{Mirrors: DefaultVPNGateMirrors}
}

func (s *VPNGateMirrorsSource) ID() string   { return "vpngate_mirrors" }
func (s *VPNGateMirrorsSource) Name() string { return "VPN Gate Mirror" }
func (s *VPNGateMirrorsSource) Tag() string  { return "VGM" }

func (s *VPNGateMirrorsSource) Fetch(ctx context.Context, client *http.Client) ([]core.Server, error) {
	var lastErr error
	for _, mirror := range s.Mirrors {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, mirror, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "MyOVPN/1.1")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		servers, parseErr := ParseVPNGateCSV(resp.Body, s.ID(), s.Tag())
		_ = resp.Body.Close()
		if parseErr == nil && len(servers) > 0 {
			return servers, nil
		}
		if parseErr != nil {
			lastErr = parseErr
		}
	}
	if lastErr == nil {
		lastErr = errors.New("all vpngate mirrors failed")
	}
	return nil, lastErr
}

// ParseVPNGateCSV parses the VPN Gate `/api/iphone/` CSV stream into normalized servers.
func ParseVPNGateCSV(r io.Reader, sourceID, sourceTag string) ([]core.Server, error) {
	br := bufio.NewReader(io.LimitReader(r, 32<<20))
	var header []string
	var csvLines []string

	for {
		line, err := br.ReadString('\n')
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "*") {
			if strings.HasPrefix(trimmed, "#HostName") {
				trimmed = strings.TrimPrefix(trimmed, "#")
				cr := csv.NewReader(strings.NewReader(trimmed))
				header, _ = cr.Read()
			} else if !strings.HasPrefix(trimmed, "#") && header != nil {
				csvLines = append(csvLines, trimmed)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}

	if len(header) == 0 {
		return nil, errors.New("missing vpngate CSV header")
	}

	col := make(map[string]int, len(header))
	for i, h := range header {
		col[strings.TrimSpace(h)] = i
	}
	required := []string{"IP", "CountryShort", "CountryLong", "OpenVPN_ConfigData_Base64"}
	for _, reqCol := range required {
		if _, ok := col[reqCol]; !ok {
			return nil, fmt.Errorf("vpngate CSV missing column %s", reqCol)
		}
	}

	var out []core.Server
	for _, rawLine := range csvLines {
		cr := csv.NewReader(strings.NewReader(rawLine))
		cr.FieldsPerRecord = -1
		rec, err := cr.Read()
		if err != nil || len(rec) < len(header) {
			continue
		}

		ip := strings.TrimSpace(rec[col["IP"]])
		cc := strings.ToUpper(strings.TrimSpace(rec[col["CountryShort"]]))
		cn := strings.TrimSpace(rec[col["CountryLong"]])
		b64 := strings.TrimSpace(rec[col["OpenVPN_ConfigData_Base64"]])
		if ip == "" || cc == "" || b64 == "" {
			continue
		}

		ovpnBytes, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			continue
		}

		sanitized, err := ovpn.SanitizeAndValidate(string(ovpnBytes), ovpn.SanitizeOptions{
			ExpectedIP: ip,
		})
		if err != nil {
			continue
		}

		port := sanitized.RemotePort
		var portPtr *int
		if port > 0 {
			p := port
			portPtr = &p
		}

		srv := core.Server{
			ID:           fmt.Sprintf("%s-%s-%s-%d", sourceID, cc, ip, port),
			Source:       sourceID,
			SourceTag:    sourceTag,
			CountryCode:  cc,
			CountryName:  cn,
			IP:           ip,
			Port:         portPtr,
			Proto:        sanitized.Proto,
			SpeedBps:     parseOptInt64(rec, col, "Speed"),
			PingMs:       parseOptInt(rec, col, "Ping"),
			Sessions:     parseOptInt(rec, col, "NumVpnSessions"),
			UptimeMs:     parseOptInt64(rec, col, "Uptime"),
			OVPNText:     sanitized.ConfigText,
			Auth:         core.AuthNone,
			StrongCipher: sanitized.StrongCipher,
		}
		out = append(out, srv)
	}

	return out, nil
}

func parseOptInt(rec []string, col map[string]int, name string) *int {
	idx, ok := col[name]
	if !ok || idx >= len(rec) {
		return nil
	}
	v, err := strconv.Atoi(strings.TrimSpace(rec[idx]))
	if err != nil {
		return nil
	}
	return &v
}

func parseOptInt64(rec []string, col map[string]int, name string) *int64 {
	idx, ok := col[name]
	if !ok || idx >= len(rec) {
		return nil
	}
	v, err := strconv.ParseInt(strings.TrimSpace(rec[idx]), 10, 64)
	if err != nil {
		return nil
	}
	return &v
}
