package sources

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/uri-travoski/MyOVPN/internal/core"
)

// Source is a pluggable provider of normalized OpenVPN servers.
type Source interface {
	ID() string
	Name() string
	Tag() string
	Fetch(ctx context.Context, client *http.Client) ([]core.Server, error)
}

// FetchResult summarizes the merged server list and which sources succeeded.
type FetchResult struct {
	Servers    []core.Server
	SourceUsed string
	UsingCache bool
	CacheAge   time.Duration
	Errors     map[string]error
}

// FetchAll fetches servers according to sourceOrder and enabled flags, caching to cachePath.
// VPN Gate mirrors are skipped unless the primary VPN Gate source fails.
func FetchAll(
	ctx context.Context,
	client *http.Client,
	sourceOrder []string,
	enabled map[string]bool,
	customURLs []string,
	importDir string,
	cachePath string,
	forceRefresh bool,
) FetchResult {
	if client == nil {
		client = &http.Client{Timeout: 25 * time.Second}
	}
	now := time.Now()

	if !forceRefresh && cachePath != "" {
		if cached, fresh, err := core.LoadServerCache(cachePath, now); err == nil && fresh && len(cached.Servers) > 0 {
			return FetchResult{
				Servers:    cached.Servers,
				SourceUsed: cached.Source,
				UsingCache: false,
				CacheAge:   now.Sub(cached.FetchedAt),
			}
		}
	}

	registry := map[string]Source{
		"vpngate":         NewVPNGateSource(),
		"vpngate_mirrors": NewVPNGateMirrorsSource(),
		"vpnbook":         NewVPNBookSource(),
		"freeopenvpn":     NewFreeOpenVPNSource(),
		"vpnobratno":      NewVPNObratnoSource(),
		"imported":        NewImportedSource(importDir),
		"custom_url":      NewCustomURLsSource(customURLs),
	}

	var merged []core.Server
	var usedNames []string
	errs := make(map[string]error)
	vpngateOK := false

	for _, id := range sourceOrder {
		if id == "cache" {
			continue
		}
		if on, exists := enabled[id]; exists && !on {
			continue
		}
		if id == "vpngate_mirrors" && vpngateOK {
			// PRD: Use mirrors only when #1 is unreachable
			continue
		}
		src, ok := registry[id]
		if !ok {
			continue
		}
		servers, err := src.Fetch(ctx, client)
		if err != nil {
			errs[id] = err
			continue
		}
		if len(servers) > 0 {
			if id == "vpngate" {
				vpngateOK = true
			}
			merged = append(merged, servers...)
			usedNames = append(usedNames, src.Name())
		}
	}

	merged = core.DedupeServers(merged)

	if len(merged) > 0 {
		sourceLabel := "Online"
		if len(usedNames) > 0 {
			sourceLabel = usedNames[0]
			if len(usedNames) > 1 {
				sourceLabel = fmt.Sprintf("%s (+%d)", usedNames[0], len(usedNames)-1)
			}
		}
		if cachePath != "" {
			_ = core.SaveServerCache(cachePath, core.CachedServerList{
				FetchedAt: now,
				Source:    sourceLabel,
				Servers:   merged,
			})
		}
		return FetchResult{
			Servers:    merged,
			SourceUsed: sourceLabel,
			UsingCache: false,
			Errors:     errs,
		}
	}

	// Fallback to local cache when every online source fails (FR-1)
	if cachePath != "" && enabled["cache"] {
		if cached, _, err := core.LoadServerCache(cachePath, now); err == nil && len(cached.Servers) > 0 {
			return FetchResult{
				Servers:    cached.Servers,
				SourceUsed: "Cache (" + cached.Source + ")",
				UsingCache: true,
				CacheAge:   now.Sub(cached.FetchedAt),
				Errors:     errs,
			}
		}
	}

	return FetchResult{
		Errors: errs,
	}
}
