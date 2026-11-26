package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/uri-travoski/MyOVPN/internal/config"
)

const LatestReleaseAPI = "https://api.github.com/repos/uri-travoski/MyOVPN/releases/latest"

// ReleaseAsset describes a single downloadable asset attached to a GitHub Release.
type ReleaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// CheckResult summarizes a GitHub latest-release check.
type CheckResult struct {
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
	ReleaseURL      string
	DownloadURL     string
	ReleaseNotes    string
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// CheckLatest queries GitHub Releases API (or apiURL override in tests) and compares versions.
func CheckLatest(ctx context.Context, client *http.Client, apiURL, currentVersion string) (CheckResult, error) {
	if apiURL == "" {
		apiURL = LatestReleaseAPI
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return CheckResult{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "MyOVPN/"+strings.TrimPrefix(currentVersion, "v"))

	resp, err := client.Do(req)
	if err != nil {
		return CheckResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return CheckResult{}, fmt.Errorf("GitHub API HTTP %d", resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return CheckResult{}, err
	}

	latest := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	if latest == "" {
		latest = strings.TrimPrefix(strings.TrimSpace(rel.Name), "v")
	}
	curr := strings.TrimPrefix(strings.TrimSpace(currentVersion), "v")

	assets := make([]ReleaseAsset, 0, len(rel.Assets))
	for _, a := range rel.Assets {
		assets = append(assets, ReleaseAsset{Name: a.Name, URL: a.BrowserDownloadURL})
	}

	dlURL := PickDownloadURL(assets, rel.HTMLURL)
	return CheckResult{
		CurrentVersion:  curr,
		LatestVersion:   latest,
		UpdateAvailable: CompareVersions(curr, latest) < 0,
		ReleaseURL:      rel.HTMLURL,
		DownloadURL:     dlURL,
		ReleaseNotes:    rel.Body,
	}, nil
}

// CompareVersions returns -1 if a < b, 0 if a == b, and 1 if a > b for dotted numeric semver strings.
func CompareVersions(a, b string) int {
	pa := parseParts(a)
	pb := parseParts(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		va, vb := 0, 0
		if i < len(pa) {
			va = pa[i]
		}
		if i < len(pb) {
			vb = pb[i]
		}
		if va < vb {
			return -1
		}
		if va > vb {
			return 1
		}
	}
	return 0
}

func parseParts(v string) []int {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		v = v[:idx]
	}
	if v == "" {
		return nil
	}
	segs := strings.Split(v, ".")
	out := make([]int, 0, len(segs))
	for _, s := range segs {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}

// PickDownloadURL selects the best asset URL for the running OS/arch/package format, falling back to releaseURL.
func PickDownloadURL(assets []ReleaseAsset, releaseURL string) string {
	var want []string
	switch runtime.GOOS {
	case "windows":
		if runtime.GOARCH == "arm64" {
			want = []string{"Setup-", "arm64.exe"}
		} else {
			want = []string{"Setup-", "x64.exe"}
		}
	case "linux":
		switch {
		case config.IsAppImage():
			if runtime.GOARCH == "arm64" {
				want = []string{"aarch64.AppImage"}
			} else {
				want = []string{"x86_64.AppImage"}
			}
		case config.IsFlatpak():
			if runtime.GOARCH == "arm64" {
				want = []string{"aarch64.flatpak"}
			} else {
				want = []string{"x86_64.flatpak"}
			}
		default:
			if runtime.GOARCH == "arm64" {
				want = []string{"arm64.deb"}
			} else {
				want = []string{"amd64.deb"}
			}
		}
	}

	if len(want) > 0 {
		for _, a := range assets {
			matched := true
			for _, sub := range want {
				if !strings.Contains(a.Name, sub) {
					matched = false
					break
				}
			}
			if matched && a.URL != "" {
				return a.URL
			}
		}
	}
	return releaseURL
}
