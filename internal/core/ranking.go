package core

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// FilterOptions configures server filtering and scoring thresholds.
type FilterOptions struct {
	MinSpeedMbps     float64
	MinUptimeMinutes int
}

// DefaultFilterOptions returns the PRD filtering defaults.
func DefaultFilterOptions() FilterOptions {
	return FilterOptions{
		MinSpeedMbps:     5.0,
		MinUptimeMinutes: 30,
	}
}

// ComputeScore calculates the PRD ranking score for a server.
// Servers with speed data use: (speed_mbps / (sessions + 1)) * (1 / (1 + ping_ms / 200)).
// Servers without speed data use: 1 / (1 + ping_ms / 200).
func ComputeScore(s *Server) float64 {
	ping := 100.0
	if s.PingMs != nil && *s.PingMs >= 0 {
		ping = float64(*s.PingMs)
	}
	pingFactor := 1.0 / (1.0 + ping/200.0)

	if s.SpeedBps == nil || *s.SpeedBps <= 0 {
		return pingFactor
	}
	sessions := 0.0
	if s.Sessions != nil && *s.Sessions >= 0 {
		sessions = float64(*s.Sessions)
	}
	return (s.SpeedMbps() / (sessions + 1.0)) * pingFactor
}

// IsUsable returns true if the server meets minimum filtering criteria and is not blacklisted.
func IsUsable(s Server, opts FilterOptions, bl *Blacklist, now time.Time) bool {
	if strings.TrimSpace(s.OVPNText) == "" {
		return false
	}
	if strings.TrimSpace(s.IP) == "" && strings.TrimSpace(s.Hostname) == "" {
		return false
	}
	if bl != nil && bl.IsBlacklisted(s.IP, now) {
		return false
	}
	if s.SpeedBps != nil {
		minBps := int64(opts.MinSpeedMbps * 1_000_000.0)
		if *s.SpeedBps < minBps {
			return false
		}
	}
	if s.UptimeMs != nil && opts.MinUptimeMinutes > 0 {
		minMs := int64(opts.MinUptimeMinutes) * 60_000
		if *s.UptimeMs < minMs {
			return false
		}
	}
	return true
}

// DedupeServers removes duplicate servers by IP (preserving first/highest-priority source order).
func DedupeServers(servers []Server) []Server {
	seen := make(map[string]struct{}, len(servers))
	out := make([]Server, 0, len(servers))
	for _, s := range servers {
		key := strings.TrimSpace(s.IP)
		if key == "" {
			key = strings.TrimSpace(s.Hostname)
		}
		if key == "" {
			continue
		}
		if s.Port != nil && s.Source != "vpngate" && s.Source != "vpngate_mirrors" {
			// Include port/proto for multi-port servers like VPNBook, dedupe exact endpoint
			key = key + ":" + s.Proto
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	return out
}

// FilterAndRank filters servers for a country (or all if countryCode==""), computes scores, and sorts descending.
func FilterAndRank(servers []Server, countryCode string, opts FilterOptions, bl *Blacklist, now time.Time) []Server {
	cc := strings.ToUpper(strings.TrimSpace(countryCode))
	out := make([]Server, 0, len(servers))
	for _, s := range servers {
		if cc != "" && !strings.EqualFold(s.CountryCode, cc) {
			continue
		}
		if !IsUsable(s, opts, bl, now) {
			continue
		}
		s.Score = ComputeScore(&s)
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		// Imported / trusted sources or speed-bearing servers rank before speedless scrape sources
		hi := out[i].SpeedBps != nil && *out[i].SpeedBps > 0
		hj := out[j].SpeedBps != nil && *out[j].SpeedBps > 0
		if hi != hj {
			return hi
		}
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// TopCandidates returns up to n highest-ranked usable servers for the given country.
func TopCandidates(servers []Server, countryCode string, n int, opts FilterOptions, bl *Blacklist, now time.Time) []Server {
	ranked := FilterAndRank(servers, countryCode, opts, bl, now)
	if n <= 0 || len(ranked) <= n {
		return ranked
	}
	return ranked[:n]
}

// SummarizeCountries aggregates usable servers by country, pinning recentCountries first, then sorting by count desc.
func SummarizeCountries(servers []Server, recentCountries []string, opts FilterOptions, bl *Blacklist, now time.Time) []CountrySummary {
	counts := make(map[string]int)
	names := make(map[string]string)

	for _, s := range servers {
		if !IsUsable(s, opts, bl, now) {
			continue
		}
		cc := strings.ToUpper(strings.TrimSpace(s.CountryCode))
		if cc == "" {
			continue
		}
		counts[cc]++
		if names[cc] == "" && s.CountryName != "" {
			names[cc] = s.CountryName
		}
	}

	pinnedOrder := make(map[string]int)
	for i, r := range recentCountries {
		rc := strings.ToUpper(strings.TrimSpace(r))
		if rc != "" {
			if _, exists := pinnedOrder[rc]; !exists {
				pinnedOrder[rc] = i
			}
		}
	}

	summaries := make([]CountrySummary, 0, len(counts))
	for cc, cnt := range counts {
		name := names[cc]
		if name == "" {
			name = cc
		}
		_, isPinned := pinnedOrder[cc]
		summaries = append(summaries, CountrySummary{
			Code:        cc,
			Name:        name,
			Flag:        FlagEmoji(cc),
			ServerCount: cnt,
			Pinned:      isPinned,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		pi, okI := pinnedOrder[summaries[i].Code]
		pj, okJ := pinnedOrder[summaries[j].Code]
		if okI != okJ {
			return okI
		}
		if okI && okJ && pi != pj {
			return pi < pj
		}
		if summaries[i].ServerCount != summaries[j].ServerCount {
			return summaries[i].ServerCount > summaries[j].ServerCount
		}
		return summaries[i].Name < summaries[j].Name
	})

	return summaries
}

// PaginateCountries returns a 1-based page slice (10 per page by default) and total pages.
func PaginateCountries(countries []CountrySummary, page, pageSize int) ([]CountrySummary, int, int) {
	if pageSize <= 0 {
		pageSize = 10
	}
	totalPages := (len(countries) + pageSize - 1) / pageSize
	if totalPages == 0 {
		return nil, 1, 1
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if end > len(countries) {
		end = len(countries)
	}
	return countries[start:end], page, totalPages
}

// Blacklist tracks temporarily failed server IPs (FR-4: 60 minutes).
type Blacklist struct {
	mu      sync.Mutex
	entries map[string]time.Time
	ttl     time.Duration
}

// NewBlacklist creates a Blacklist with the given TTL.
func NewBlacklist(ttl time.Duration) *Blacklist {
	if ttl <= 0 {
		ttl = 60 * time.Minute
	}
	return &Blacklist{
		entries: make(map[string]time.Time),
		ttl:     ttl,
	}
}

// Add blacklists an IP for the configured TTL.
func (b *Blacklist) Add(ip string, now time.Time) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries[ip] = now.Add(b.ttl)
}

// IsBlacklisted returns true if ip is currently blacklisted.
func (b *Blacklist) IsBlacklisted(ip string, now time.Time) bool {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	exp, ok := b.entries[ip]
	if !ok {
		return false
	}
	if now.After(exp) {
		delete(b.entries, ip)
		return false
	}
	return true
}

// Clear removes all blacklisted entries.
func (b *Blacklist) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries = make(map[string]time.Time)
}
