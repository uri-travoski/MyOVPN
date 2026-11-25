package sources

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/uri-travoski/MyOVPN/internal/core"
	"github.com/uri-travoski/MyOVPN/internal/ovpn"
)

const DefaultVPNBookURL = "https://www.vpnbook.com/freevpn/openvpn"

var (
	vpnbookCredRegex = regexp.MustCompile(`(?i)Username:\s*([a-zA-Z0-9_-]+)[.\s,;]+Password:\s*([a-zA-Z0-9_-]+)`)
	vpnbookRowRegex  = regexp.MustCompile(`(?is)([a-z]{2}\d+\.vpnbook\.com)\s*</th>\s*<td[^>]*>\s*([^<]+?)\s*</td>`)
	vpnbookHostRegex = regexp.MustCompile(`(?i)\b([a-z]{2}\d+\.vpnbook\.com)\b`)
)

// VPNBookSource scrapes credentials & server list from VPNBook's OpenVPN page
// and fetches ready-to-use .ovpn configs via `/api/openvpn`.
type VPNBookSource struct {
	BaseURL string
}

func NewVPNBookSource() *VPNBookSource {
	return &VPNBookSource{BaseURL: "https://www.vpnbook.com"}
}

func (s *VPNBookSource) ID() string   { return "vpnbook" }
func (s *VPNBookSource) Name() string { return "VPNBook" }
func (s *VPNBookSource) Tag() string  { return "VB" }

func (s *VPNBookSource) Fetch(ctx context.Context, client *http.Client) ([]core.Server, error) {
	pageURL := strings.TrimRight(s.BaseURL, "/") + "/freevpn/openvpn"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vpnbook HTTP %d", resp.StatusCode)
	}

	html := string(body)
	username, password, hosts := ParseVPNBookHTML(html)
	if username == "" || password == "" || len(hosts) == 0 {
		return nil, fmt.Errorf("vpnbook parse failed (user=%q hosts=%d)", username, len(hosts))
	}

	var out []core.Server
	for _, h := range hosts {
		for _, proto := range []string{"tcp443", "udp25000"} {
			ovpnURL := fmt.Sprintf("%s/api/openvpn?hostname=%s&protocol=%s",
				strings.TrimRight(s.BaseURL, "/"),
				url.QueryEscape(h.Hostname),
				url.QueryEscape(proto),
			)
			ovpnReq, err := http.NewRequestWithContext(ctx, http.MethodGet, ovpnURL, nil)
			if err != nil {
				continue
			}
			ovpnReq.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")
			ovpnResp, err := client.Do(ovpnReq)
			if err != nil {
				continue
			}
			raw, err := io.ReadAll(io.LimitReader(ovpnResp.Body, 512<<10))
			_ = ovpnResp.Body.Close()
			if err != nil || ovpnResp.StatusCode != http.StatusOK {
				continue
			}

			ip := resolveIPv4(ctx, h.Hostname)
			if ip == "" {
				ip = h.Hostname
			}

			sanitized, err := ovpn.SanitizeAndValidate(string(raw), ovpn.SanitizeOptions{
				ExpectedHostname: h.Hostname,
				ExpectedIP:       ip,
			})
			if err != nil {
				continue
			}

			ping := measureTCPPing(ctx, h.Hostname, sanitized.RemotePort)
			var pingPtr *int
			if ping >= 0 {
				p := ping
				pingPtr = &p
			}
			port := sanitized.RemotePort
			var portPtr *int
			if port > 0 {
				p := port
				portPtr = &p
			}

			out = append(out, core.Server{
				ID:           fmt.Sprintf("vpnbook-%s-%s-%d", h.CountryCode, h.Hostname, port),
				Source:       s.ID(),
				SourceTag:    s.Tag(),
				CountryCode:  h.CountryCode,
				CountryName:  h.CountryName,
				IP:           ip,
				Hostname:     h.Hostname,
				Port:         portPtr,
				Proto:        sanitized.Proto,
				SpeedBps:     nil,
				PingMs:       pingPtr,
				Sessions:     nil,
				UptimeMs:     nil,
				OVPNText:     sanitized.ConfigText,
				Auth:         core.AuthUserPass,
				Username:     username,
				Password:     password,
				StrongCipher: sanitized.StrongCipher,
			})
		}
	}

	return out, nil
}

type vpnbookHost struct {
	Hostname    string
	CountryCode string
	CountryName string
}

// ParseVPNBookHTML extracts username, password, and server hostnames from VPNBook HTML.
func ParseVPNBookHTML(html string) (username, password string, hosts []vpnbookHost) {
	if m := vpnbookCredRegex.FindStringSubmatch(html); len(m) == 3 {
		username = strings.TrimSpace(m[1])
		password = strings.TrimSpace(m[2])
	}

	seen := make(map[string]bool)
	for _, m := range vpnbookRowRegex.FindAllStringSubmatch(html, -1) {
		host := strings.ToLower(strings.TrimSpace(m[1]))
		cname := strings.TrimSpace(m[2])
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		cc := strings.ToUpper(host[:2])
		if cc == "UK" {
			cc = "GB"
		}
		hosts = append(hosts, vpnbookHost{
			Hostname:    host,
			CountryCode: cc,
			CountryName: cname,
		})
	}

	if len(hosts) == 0 {
		for _, m := range vpnbookHostRegex.FindAllStringSubmatch(html, -1) {
			host := strings.ToLower(strings.TrimSpace(m[1]))
			if seen[host] {
				continue
			}
			seen[host] = true
			cc := strings.ToUpper(host[:2])
			cname := countryNameFromCode(cc)
			if cc == "UK" {
				cc = "GB"
				cname = "United Kingdom"
			}
			hosts = append(hosts, vpnbookHost{
				Hostname:    host,
				CountryCode: cc,
				CountryName: cname,
			})
		}
	}

	return username, password, hosts
}

func resolveIPv4(ctx context.Context, host string) string {
	r := &net.Resolver{}
	ips, err := r.LookupIP(ctx, "ip4", host)
	if err != nil || len(ips) == 0 {
		return ""
	}
	return ips[0].String()
}

func measureTCPPing(ctx context.Context, host string, port int) int {
	if port <= 0 {
		port = 443
	}
	d := net.Dialer{Timeout: 2 * time.Second}
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	if err != nil {
		return -1
	}
	_ = conn.Close()
	return int(time.Since(start).Milliseconds())
}

func countryNameFromCode(cc string) string {
	switch strings.ToUpper(cc) {
	case "US":
		return "United States"
	case "CA":
		return "Canada"
	case "GB", "UK":
		return "United Kingdom"
	case "DE":
		return "Germany"
	case "FR":
		return "France"
	case "PL":
		return "Poland"
	case "JP":
		return "Japan"
	case "KR":
		return "Korea"
	case "RU":
		return "Russia"
	case "NL":
		return "Netherlands"
	case "CH":
		return "Switzerland"
	default:
		return cc
	}
}
