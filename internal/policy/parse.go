package policy

import (
	"encoding/json"
	"fmt"
	"io"

	toml "github.com/pelletier/go-toml/v2"
)

type tomlWrapper struct {
	Agents        Policy `toml:"agents"`
	HarnessPolicy Policy `toml:"harnessPolicy"`
}

// ParseTOML parses a policy from a TOML reader. It transparently supports both
// a root table (standalone policy) and a wrapped [agents] or [harnessPolicy] table (.chezmoidata source).
func ParseTOML(r io.Reader) (*Policy, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read policy: %w", err)
	}

	var w tomlWrapper
	if err := toml.Unmarshal(b, &w); err == nil {
		if !w.Agents.isEmpty() {
			return &w.Agents, nil
		}
		if !w.HarnessPolicy.isEmpty() {
			return &w.HarnessPolicy, nil
		}
	}

	var p Policy
	if err := toml.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("parse policy toml: %w", err)
	}
	return &p, nil
}

// FromData converts an untyped chezmoi data map into a typed Policy struct.
func FromData(data map[string]any) (*Policy, error) {
	target := data
	if sub, ok := data["agents"].(map[string]any); ok {
		target = sub
	} else if sub, ok := data["harnessPolicy"].(map[string]any); ok {
		target = sub
	}

	b, err := json.Marshal(target)
	if err != nil {
		return nil, fmt.Errorf("marshal policy map: %w", err)
	}

	var p Policy
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("unmarshal policy struct: %w", err)
	}
	return &p, nil
}

func (p Policy) isEmpty() bool {
	return len(p.Permissions.DenyRead) == 0 &&
		len(p.Permissions.DenyWrite) == 0 &&
		len(p.Permissions.DenyCommandPatterns) == 0 &&
		len(p.Permissions.DenyCommandExact) == 0 &&
		len(p.Permissions.DenyCommandPrefixes) == 0 &&
		len(p.Permissions.AskCommandPrefixes) == 0 &&
		p.Native.Claude.DefaultMode == "" &&
		p.Native.Antigravity.AgentMode == ""
}
