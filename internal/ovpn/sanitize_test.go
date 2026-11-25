package ovpn

import (
	"strings"
	"testing"
)

func TestSanitizeStripsDangerousDirectivesAndFixesDataCiphers(t *testing.T) {
	raw := `
client
dev tun1
proto tcp
remote 1.2.3.4 443
cipher AES-128-CBC
auth SHA1
script-security 2
up /tmp/evil.sh
down "/tmp/evil2.sh"
plugin /tmp/evil.so
route-up /tmp/evil3.sh
tls-verify /tmp/evil4.sh
<ca>
-----BEGIN CERTIFICATE-----
MIIB
-----END CERTIFICATE-----
</ca>
`
	res, err := SanitizeAndValidate(raw, SanitizeOptions{
		ExpectedIP:   "1.2.3.4",
		ForceTunName: "tun0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, bad := range []string{"script-security", "evil.sh", "evil.so", "plugin", "route-up", "tls-verify"} {
		if strings.Contains(res.ConfigText, bad) {
			t.Fatalf("dangerous string %q survived sanitization:\n%s", bad, res.ConfigText)
		}
	}
	if !strings.Contains(res.ConfigText, "data-ciphers "+ModernDataCiphers) {
		t.Fatalf("missing OpenVPN 2.6 data-ciphers in:\n%s", res.ConfigText)
	}
	if !strings.Contains(res.ConfigText, "dev tun0") {
		t.Fatalf("expected ForceTunName tun0")
	}
	if res.StrongCipher {
		t.Fatalf("expected AES-128-CBC server to have StrongCipher=false")
	}
}

func TestSanitizeRejectsMismatchedRemote(t *testing.T) {
	raw := "client\ndev tun\nremote 9.9.9.9 1194\n"
	_, err := SanitizeAndValidate(raw, SanitizeOptions{ExpectedIP: "1.2.3.4"})
	if err == nil {
		t.Fatal("expected error for mismatched remote IP")
	}
}

func TestSanitizeStrongCipherDetection(t *testing.T) {
	raw := "client\ndev tun\nremote us16.vpnbook.com 443\ncipher AES-256-GCM\ndata-ciphers AES-256-GCM:AES-128-GCM\n"
	res, err := SanitizeAndValidate(raw, SanitizeOptions{ExpectedHostname: "us16.vpnbook.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.StrongCipher {
		t.Fatal("expected AES-256-GCM to be marked StrongCipher=true")
	}
}
