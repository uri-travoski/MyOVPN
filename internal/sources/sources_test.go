package sources

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseVPNGateSampleCSV(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "vpngate_sample.csv"))
	if err != nil {
		t.Fatalf("open sample csv: %v", err)
	}
	defer f.Close()

	servers, err := ParseVPNGateCSV(f, "vpngate", "VG")
	if err != nil {
		t.Fatalf("ParseVPNGateCSV error: %v", err)
	}
	if len(servers) == 0 {
		t.Fatal("expected parsed servers from sample CSV")
	}
	for _, s := range servers {
		if s.IP == "" || s.CountryCode == "" || s.OVPNText == "" {
			t.Fatalf("incomplete server record: %+v", s)
		}
		if s.SpeedBps == nil || s.Sessions == nil || s.UptimeMs == nil {
			t.Fatalf("expected non-null metrics in vpngate sample: %+v", s)
		}
	}
}

func TestParseVPNBookHTML(t *testing.T) {
	sample := `
	{"@type":"Answer","text":"Username: vpnbook. Password: ztene8z. The same login works."}
	<table>
	<tr><th>us16.vpnbook.com</th><td>United States</td></tr>
	<tr><th>de20.vpnbook.com</th><td>Germany</td></tr>
	<tr><th>uk205.vpnbook.com</th><td>United Kingdom</td></tr>
	</table>`
	u, p, hosts := ParseVPNBookHTML(sample)
	if u != "vpnbook" || p != "ztene8z" {
		t.Fatalf("unexpected creds: %q / %q", u, p)
	}
	if len(hosts) != 3 {
		t.Fatalf("expected 3 hosts, got %d", len(hosts))
	}
	if hosts[2].CountryCode != "GB" {
		t.Fatalf("expected uk205 normalized to GB, got %s", hosts[2].CountryCode)
	}
}
