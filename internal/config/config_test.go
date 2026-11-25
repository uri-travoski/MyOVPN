package config

import (
	"path/filepath"
	"testing"
)

func TestConfigSaveLoadAndNormalize(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load default: %v", err)
	}
	cfg.MaxProfiles = 99
	cfg.RecordRecentCountry("jp")
	cfg.RecordRecentCountry("us")
	cfg.RecordRecentCountry("kr")
	cfg.RecordRecentCountry("de")
	cfg.RecordRecentCountry("us")

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if reloaded.MaxProfiles != 5 {
		t.Fatalf("expected MaxProfiles clamped to 5, got %d", reloaded.MaxProfiles)
	}
	if len(reloaded.RecentCountries) != 3 ||
		reloaded.RecentCountries[0] != "US" ||
		reloaded.RecentCountries[1] != "DE" ||
		reloaded.RecentCountries[2] != "KR" {
		t.Fatalf("unexpected RecentCountries: %v", reloaded.RecentCountries)
	}
}
