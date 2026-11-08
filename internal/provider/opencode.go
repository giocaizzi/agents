package provider

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// GitHub Copilot enterprise logins: opencode stores the URL as
// metadata.enterpriseUrl (the form prompts "company.ghe.com or https://...").
// Source: https://github.com/anomalyco/opencode/blob/HEAD/packages/core/src/plugin/provider/github-copilot.ts
type opencodeValue struct {
	Type     string  `json:"type"` // oauth | key | external
	Access   string  `json:"access"`
	Refresh  string  `json:"refresh"`
	Expires  float64 `json:"expires"`
	Key      string  `json:"key"`
	Metadata struct {
		AccountID     string `json:"accountId"`
		EnterpriseURL string `json:"enterpriseUrl"`
	} `json:"metadata"`
}

// legacy auth.json entry: type oauth | api | wellknown.
type opencodeLegacy struct {
	Type      string  `json:"type"`
	Access    string  `json:"access"`
	Refresh   string  `json:"refresh"`
	Expires   float64 `json:"expires"`
	AccountID string  `json:"accountId"`
	Key       string  `json:"key"`
	// Enterprise URL of a GitHub Copilot login; the V1 store keeps it at the
	// top level.
	EnterpriseURL string `json:"enterpriseUrl"`
}

func normalizeTenantType(t string) string {
	switch t {
	case "oauth":
		return tenantOAuth
	case "key", "api", "api_key":
		return tenantKey
	default: // external, wellknown
		return tenantExternal
	}
}

// readOpencode reads the V2 credential table, else the legacy V1 auth.json.
func readOpencode(st Store) []Account {
	dataHome := st.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(st.Home(), ".local", "share")
	}
	dataDir := filepath.Join(dataHome, "opencode")

	dbs := []string{st.Getenv("OPENCODE_DB")}
	if dbs[0] == "" {
		dbs = []string{filepath.Join(dataDir, "opencode.db")}
		if st.GOOS() == "windows" {
			// Unverified: the Windows data directory is assumed to be
			// %LOCALAPPDATA%\opencode; XDG paths are tried first.
			if l := st.Getenv("LOCALAPPDATA"); l != "" {
				dbs = append(dbs, filepath.Join(l, "opencode", "opencode.db"))
			}
		}
	}

	for _, db := range dbs {
		rows, err := st.OpencodeCredentials(db)
		if err != nil {
			if isNotExist(err) {
				continue
			}
			return []Account{unreadable("opencode", harnessOpencode)}
		}
		return opencodeRowAccounts(st, rows)
	}

	// No database: the legacy store.
	var legacy map[string]opencodeLegacy
	found, err := readJSON(st, filepath.Join(dataDir, "auth.json"), &legacy)
	if !found {
		return nil
	}
	if err != nil {
		return []Account{unreadable("opencode", harnessOpencode)}
	}
	var out []Account
	for _, id := range sortedKeys(legacy) {
		l := legacy[id]
		out = append(out, tenantEntry{
			harness:   harnessOpencode,
			provider:  id,
			typ:       normalizeTenantType(l.Type),
			access:    l.Access,
			refresh:   l.Refresh,
			key:       l.Key,
			expires:   msTime(l.Expires),
			accountID: l.AccountID,
			host:      l.EnterpriseURL,
			source:    srcFile,
		}.account(st.Now()))
	}
	return out
}

func opencodeRowAccounts(st Store, rows []OpencodeRow) []Account {
	var out []Account
	for _, r := range rows {
		if strings.HasPrefix(r.ID, "mcp_") || strings.HasPrefix(r.IntegrationID, "mcp") {
			continue
		}
		var v opencodeValue
		if err := json.Unmarshal(r.Value, &v); err != nil {
			a := unreadable(r.IntegrationID, harnessOpencode)
			a.Source = srcSQLite
			out = append(out, a)
			continue
		}
		out = append(out, tenantEntry{
			harness:   harnessOpencode,
			provider:  r.IntegrationID,
			typ:       normalizeTenantType(v.Type),
			access:    v.Access,
			refresh:   v.Refresh,
			key:       v.Key,
			expires:   msTime(v.Expires),
			accountID: v.Metadata.AccountID,
			host:      v.Metadata.EnterpriseURL,
			rowID:     r.ID,
			label:     r.Label,
			inactive:  !r.Active,
			source:    srcSQLite,
		}.account(st.Now()))
	}
	return out
}
