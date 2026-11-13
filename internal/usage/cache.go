package usage

import (
	"encoding/json"
	"fmt"
	"time"
)

const cacheSchema = 1

// Cache is the content of usage.json in the agents cache dir. It never
// holds a credential.
type Cache struct {
	Schema    int        `json:"schema"`
	WrittenAt time.Time  `json:"writtenAt"`
	Snapshots []Snapshot `json:"snapshots"`
}

// MarshalCache encodes snapshots deterministically (2-space indent), sorted by
// provider then account.
func MarshalCache(snaps []Snapshot, now time.Time) ([]byte, error) {
	sorted := sortedByProviderAccount(snaps)
	if sorted == nil {
		sorted = []Snapshot{}
	}
	b, err := json.MarshalIndent(Cache{Schema: cacheSchema, WrittenAt: now, Snapshots: sorted}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode usage cache: %w", err)
	}
	return append(b, '\n'), nil
}

// ParseCache decodes usage.json; unknown fields are ignored.
func ParseCache(b []byte) (Cache, error) {
	var c Cache
	if err := json.Unmarshal(b, &c); err != nil {
		return Cache{}, fmt.Errorf("parse usage cache: %w", err)
	}
	if c.Schema != cacheSchema {
		return Cache{}, fmt.Errorf("usage cache schema %d, want %d", c.Schema, cacheSchema)
	}
	return c, nil
}
