package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const CacheTTL = 15 * time.Minute

// CachedServerList is persisted in servers.json (FR-1).
type CachedServerList struct {
	FetchedAt time.Time `json:"fetched_at"`
	Source    string    `json:"source"`
	Servers   []Server  `json:"servers"`
}

// SaveServerCache writes the merged server list atomically.
func SaveServerCache(path string, list CachedServerList) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadServerCache reads servers.json and returns whether it is within the 15-minute fresh TTL.
func LoadServerCache(path string, now time.Time) (*CachedServerList, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	var list CachedServerList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, false, err
	}
	fresh := now.Sub(list.FetchedAt) <= CacheTTL
	return &list, fresh, nil
}
