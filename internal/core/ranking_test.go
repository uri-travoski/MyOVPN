package core

import (
	"testing"
	"time"
)

func intPtr(v int) *int       { return &v }
func int64Ptr(v int64) *int64 { return &v }

func TestScoreAndRanking(t *testing.T) {
	now := time.Now()
	opts := DefaultFilterOptions()
	bl := NewBlacklist(60 * time.Minute)

	servers := []Server{
		{
			ID: "slow", Source: "vpngate", CountryCode: "JP", CountryName: "Japan",
			IP: "1.1.1.1", SpeedBps: int64Ptr(3_000_000), PingMs: intPtr(10), Sessions: intPtr(0),
			UptimeMs: int64Ptr(3_600_000), OVPNText: "remote 1.1.1.1 443",
		},
		{
			ID: "newbie", Source: "vpngate", CountryCode: "JP", CountryName: "Japan",
			IP: "1.1.1.2", SpeedBps: int64Ptr(50_000_000), PingMs: intPtr(10), Sessions: intPtr(0),
			UptimeMs: int64Ptr(600_000), OVPNText: "remote 1.1.1.2 443", // < 30 min
		},
		{
			ID: "crowded", Source: "vpngate", CountryCode: "JP", CountryName: "Japan",
			IP: "1.1.1.3", SpeedBps: int64Ptr(100_000_000), PingMs: intPtr(20), Sessions: intPtr(9),
			UptimeMs: int64Ptr(3_600_000), OVPNText: "remote 1.1.1.3 443", // 100/(9+1) = 10
		},
		{
			ID: "best", Source: "vpngate", CountryCode: "JP", CountryName: "Japan",
			IP: "1.1.1.4", SpeedBps: int64Ptr(60_000_000), PingMs: intPtr(20), Sessions: intPtr(1),
			UptimeMs: int64Ptr(3_600_000), OVPNText: "remote 1.1.1.4 443", // 60/(1+1) = 30
		},
		{
			ID: "vpnbook", Source: "vpnbook", CountryCode: "JP", CountryName: "Japan",
			IP: "1.1.1.5", SpeedBps: nil, PingMs: intPtr(5), Sessions: nil,
			UptimeMs: nil, OVPNText: "remote 1.1.1.5 443", // no speed: must rank after vpngate
		},
	}

	ranked := FilterAndRank(servers, "JP", opts, bl, now)
	if len(ranked) != 3 {
		t.Fatalf("expected 3 usable JP servers, got %d", len(ranked))
	}
	if ranked[0].ID != "best" || ranked[1].ID != "crowded" || ranked[2].ID != "vpnbook" {
		t.Fatalf("unexpected ranking order: %s, %s, %s", ranked[0].ID, ranked[1].ID, ranked[2].ID)
	}

	// Blacklist "best"
	bl.Add("1.1.1.4", now)
	rankedAfter := FilterAndRank(servers, "JP", opts, bl, now)
	if len(rankedAfter) != 2 || rankedAfter[0].ID != "crowded" {
		t.Fatalf("blacklist failed, got %+v", rankedAfter)
	}

	// After 61 minutes, blacklist expires
	rankedExpired := FilterAndRank(servers, "JP", opts, bl, now.Add(61*time.Minute))
	if len(rankedExpired) != 3 || rankedExpired[0].ID != "best" {
		t.Fatalf("blacklist expiry failed, got %+v", rankedExpired)
	}
}

func TestSummarizeAndPaginateCountries(t *testing.T) {
	now := time.Now()
	opts := DefaultFilterOptions()
	var servers []Server
	codes := []string{"JP", "KR", "US", "DE", "GB", "FR", "CA", "NL", "SG", "AU", "CH", "SE"}
	for idx, cc := range codes {
		for k := 0; k < idx+1; k++ {
			servers = append(servers, Server{
				ID: cc, Source: "vpngate", CountryCode: cc, CountryName: cc,
				IP: "10.0.0.1", SpeedBps: int64Ptr(10_000_000), UptimeMs: int64Ptr(3_600_000),
				OVPNText: "remote 10.0.0.1 443",
			})
		}
	}

	summaries := SummarizeCountries(servers, []string{"JP", "US"}, opts, nil, now)
	if len(summaries) != 12 {
		t.Fatalf("expected 12 countries, got %d", len(summaries))
	}
	if summaries[0].Code != "JP" || !summaries[0].Pinned || summaries[1].Code != "US" || !summaries[1].Pinned {
		t.Fatalf("expected JP and US pinned first, got %s, %s", summaries[0].Code, summaries[1].Code)
	}
	if summaries[2].Code != "SE" || summaries[2].ServerCount != 12 {
		t.Fatalf("expected SE (12 servers) right after pinned, got %s (%d)", summaries[2].Code, summaries[2].ServerCount)
	}

	p1, cur1, tot1 := PaginateCountries(summaries, 1, 10)
	if len(p1) != 10 || cur1 != 1 || tot1 != 2 {
		t.Fatalf("unexpected page 1: len=%d cur=%d tot=%d", len(p1), cur1, tot1)
	}
	p2, cur2, tot2 := PaginateCountries(summaries, 2, 10)
	if len(p2) != 2 || cur2 != 2 || tot2 != 2 {
		t.Fatalf("unexpected page 2: len=%d cur=%d tot=%d", len(p2), cur2, tot2)
	}
}
