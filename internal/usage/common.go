package usage

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	unitCredits        = "credits"
	unitRequests       = "requests"
	namePro            = "pro"
	nameEnterpriseSeat = "Enterprise seat"
	unnamedWindow      = "window"
)

// scopeFor is seat for team and enterprise plans, account otherwise.
func scopeFor(p Plan) Scope {
	if p.Kind == KindTeam || p.Kind == KindEnterprise {
		return ScopeSeat
	}
	return ScopeAccount
}

func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil
	}
	return &t
}

func unixTime(sec *int64) *time.Time {
	if sec == nil || *sec <= 0 {
		return nil
	}
	t := time.Unix(*sec, 0).UTC()
	return &t
}

// humanSeconds names a window length: 18000 -> "5h", 604800 -> "7d".
func humanSeconds(s int64) string {
	switch {
	case s <= 0:
		return unnamedWindow
	case s%86400 == 0:
		return fmt.Sprintf("%dd", s/86400)
	case s%3600 == 0:
		return fmt.Sprintf("%dh", s/3600)
	case s%60 == 0:
		return fmt.Sprintf("%dm", s/60)
	}
	return fmt.Sprintf("%ds", s)
}

func spaced(s string) string { return strings.ReplaceAll(s, "_", " ") }

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// numberOrString reads a JSON number or numeric string.
func numberOrString(raw json.RawMessage) (float64, bool) {
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return f, true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if _, err := fmt.Sscanf(strings.TrimSpace(s), "%g", &f); err == nil {
			return f, true
		}
	}
	return 0, false
}

func sortWindows(limits []Limit) {
	rank := func(n string) int {
		switch n {
		case "5h":
			return 0
		case "7d":
			return 1
		}
		return 2
	}
	sort.SliceStable(limits, func(i, j int) bool {
		ri, rj := rank(limits[i].Name), rank(limits[j].Name)
		if ri != rj {
			return ri < rj
		}
		return limits[i].Name < limits[j].Name
	})
}

// HumanDuration renders a duration as "41m", "2h 14m" or "1d 9h".
func HumanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
}
