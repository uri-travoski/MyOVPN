package updater

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.1.0", "1.1.1", -1},
		{"v1.1.1", "v1.1.1", 0},
		{"1.2.0", "1.1.9", 1},
		{"1.1", "1.1.1", -1},
		{"2.0.0", "1.9.9", 1},
	}
	for _, tc := range cases {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestCheckLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tag_name": "v1.2.0",
			"html_url": "https://github.com/uri-travoski/MyOVPN/releases/tag/v1.2.0",
			"assets": [
				{"name": "myovpn_1.2.0_amd64.deb", "browser_download_url": "https://example.com/myovpn_1.2.0_amd64.deb"}
			]
		}`))
	}))
	defer srv.Close()

	res, err := CheckLatest(context.Background(), srv.Client(), srv.URL, "1.1.1")
	if err != nil {
		t.Fatalf("CheckLatest error: %v", err)
	}
	if !res.UpdateAvailable || res.LatestVersion != "1.2.0" {
		t.Fatalf("expected update to 1.2.0, got %+v", res)
	}

	resSame, err := CheckLatest(context.Background(), srv.Client(), srv.URL, "1.2.0")
	if err != nil {
		t.Fatalf("CheckLatest same error: %v", err)
	}
	if resSame.UpdateAvailable {
		t.Fatalf("expected no update when already on 1.2.0, got %+v", resSame)
	}
}
