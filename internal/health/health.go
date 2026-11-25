package health

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Check verifies tunnel connectivity by issuing an HTTPS GET to checkURL.
// If netns is non-empty on Linux, it executes the check inside that network namespace via self-exec.
func Check(ctx context.Context, checkURL string, timeout time.Duration, netns string) error {
	if netns != "" {
		return checkInNetns(ctx, checkURL, timeout, netns, false)
	}
	return DoHTTPGet(ctx, checkURL, timeout)
}

// CheckGeoIP fetches the exit country code from geoURL and verifies it matches expectedCC.
func CheckGeoIP(ctx context.Context, geoURL, expectedCC string, timeout time.Duration, netns string) error {
	if strings.TrimSpace(geoURL) == "" || strings.TrimSpace(expectedCC) == "" {
		return nil
	}
	var body string
	var err error
	if netns != "" {
		body, err = fetchBodyInNetns(ctx, geoURL, timeout, netns)
	} else {
		body, err = DoHTTPGetBody(ctx, geoURL, timeout)
	}
	if err != nil {
		return err
	}
	got := strings.ToUpper(strings.TrimSpace(body))
	want := strings.ToUpper(strings.TrimSpace(expectedCC))
	if got != "" && got != want {
		return fmt.Errorf("geoip mismatch: got %q, expected %q", got, want)
	}
	return nil
}

// DoHTTPGet performs an HTTPS GET expecting a 2xx/3xx status code.
func DoHTTPGet(ctx context.Context, targetURL string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MyOVPN-Health/1.1")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	return nil
}

// DoHTTPGetBody fetches a small response body (e.g. GeoIP country code).
func DoHTTPGetBody(ctx context.Context, targetURL string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "MyOVPN-Health/1.1")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return "", fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func checkInNetns(ctx context.Context, targetURL string, timeout time.Duration, netns string, withBody bool) error {
	_, err := fetchBodyInNetnsImpl(ctx, targetURL, timeout, netns, withBody)
	return err
}

func fetchBodyInNetns(ctx context.Context, targetURL string, timeout time.Duration, netns string) (string, error) {
	return fetchBodyInNetnsImpl(ctx, targetURL, timeout, netns, true)
}

func fetchBodyInNetnsImpl(ctx context.Context, targetURL string, timeout time.Duration, netns string, printBody bool) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout+2*time.Second)
	defer cancel()

	args := []string{"netns", "exec", netns, exe, "_netns-http", "--url", targetURL, "--timeout", timeout.String()}
	if printBody {
		args = append(args, "--body")
	}
	cmd := exec.CommandContext(ctx, "ip", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("netns health check failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
