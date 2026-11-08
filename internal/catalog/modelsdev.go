package catalog

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

type modelsDevModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ReleaseDate string `json:"release_date"`
	ToolCall    bool   `json:"tool_call"`
	Modalities  struct {
		Output []string `json:"output"`
	} `json:"modalities"`
	Cost *struct {
		Input      float64 `json:"input"`
		Output     float64 `json:"output"`
		CacheRead  float64 `json:"cache_read"`
		CacheWrite float64 `json:"cache_write"`
	} `json:"cost"`
	Limit struct {
		Context int `json:"context"`
	} `json:"limit"`
	Status           string `json:"status"`
	CanonicalModelID string `json:"canonical_model_id"`
}

// ParseModelsDev reads models.dev api.json and returns the models of the given
// provider keys, sorted by provider then id.
func ParseModelsDev(data []byte, providers []string) ([]ProviderModel, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("decode models.dev: %w", err)
	}
	var out []ProviderModel
	for _, provider := range providers {
		raw, ok := top[provider]
		if !ok {
			continue
		}
		var p struct {
			Models map[string]modelsDevModel `json:"models"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("decode models.dev provider %s: %w", provider, err)
		}
		for key, m := range p.Models {
			id := m.ID
			if id == "" {
				id = key
			}
			canonical := m.CanonicalModelID
			if canonical == "" {
				canonical = provider + "/" + id
			}
			var price *Price
			if m.Cost != nil {
				price = &Price{In: m.Cost.Input, Out: m.Cost.Output, CacheRead: m.Cost.CacheRead, CacheWrite: m.Cost.CacheWrite}
			}
			out = append(out, ProviderModel{
				Provider:    provider,
				ID:          id,
				Name:        m.Name,
				CanonicalID: canonical,
				ReleaseDate: m.ReleaseDate,
				Context:     m.Limit.Context,
				Price:       price,
				ToolCall:    m.ToolCall,
				TextOutput:  slices.Contains(m.Modalities.Output, "text"),
				Deprecated:  m.Status == "deprecated",
			})
		}
	}
	slices.SortFunc(out, func(a, b ProviderModel) int {
		if c := strings.Compare(a.Provider, b.Provider); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}
