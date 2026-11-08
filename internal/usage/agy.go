package usage

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type agyUsage struct {
	Status   string `json:"status"`
	NumTurns int    `json:"num_turns"`
	Command  *struct {
		Name string `json:"name"`
		Data struct {
			Groups []struct {
				Name    string `json:"name"`
				Buckets []struct {
					Window            string   `json:"window"`
					RemainingFraction *float64 `json:"remaining_fraction"`
					ResetTime         string   `json:"reset_time"`
					Disabled          bool     `json:"disabled"`
				} `json:"buckets"`
			} `json:"groups"`
		} `json:"data"`
	} `json:"command"`
}

func agyGroupShort(name string) string {
	name = strings.TrimSpace(name)
	for _, s := range []string{" Models", " models"} {
		name = strings.TrimSuffix(name, s)
	}
	return name
}

// ParseAgyUsage parses `agy -p /usage --output-format json`. It fails unless
// command.name is "usage" and num_turns is 0.
func ParseAgyUsage(body []byte) ([]Limit, error) {
	var u agyUsage
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, fmt.Errorf("parse agy usage: %w", err)
	}
	if u.Status != "SUCCESS" || u.Command == nil || u.Command.Name != "usage" || u.NumTurns != 0 {
		return nil, errors.New("agy did not answer /usage")
	}
	var limits []Limit
	for _, g := range u.Command.Data.Groups {
		short := agyGroupShort(g.Name)
		for _, b := range g.Buckets {
			win := b.Window
			if win == "weekly" {
				win = "7d"
			}
			l := Limit{Name: short + " · " + win, Kind: LimitWindow, Scope: ScopeAccount, Disabled: b.Disabled, ResetsAt: parseTime(b.ResetTime)}
			if b.RemainingFraction != nil {
				l.UsedPct = new(100 * (1 - *b.RemainingFraction))
			}
			limits = append(limits, l)
		}
	}
	return limits, nil
}

var agyLogLine = regexp.MustCompile(`^[EWIF](\d{2})(\d{2}) (\d{2}):(\d{2}):(\d{2})\.\d+\s.*RESOURCE_EXHAUSTED \(code 429\):.*Resets in (\S+)\s*$`)

// ParseAgyLog scans an Antigravity CLI log for the last
// "RESOURCE_EXHAUSTED (code 429): ... Resets in 33h5m2s." line and returns an
// exhausted window resetting then. logStart is the time in the log file name
// (cli-YYYYMMDD_HHMMSS.log); log lines carry month, day and time but no year.
func ParseAgyLog(r io.Reader, logStart time.Time) ([]Limit, time.Time, error) {
	br := bufio.NewReader(r)
	var last []string
	for {
		line, err := br.ReadString('\n')
		if strings.Contains(line, "RESOURCE_EXHAUSTED") {
			if m := agyLogLine.FindStringSubmatch(strings.TrimRight(line, "\r\n")); m != nil {
				last = m
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return nil, time.Time{}, fmt.Errorf("read agy log: %w", err)
			}
			break
		}
	}
	if last == nil {
		return nil, time.Time{}, nil
	}
	var n [5]int
	for i := range n {
		n[i], _ = strconv.Atoi(last[i+1])
	}
	at := time.Date(logStart.Year(), time.Month(n[0]), n[1], n[2], n[3], n[4], 0, logStart.Location())
	if at.Before(logStart) {
		at = at.AddDate(1, 0, 0)
	}
	d, err := time.ParseDuration(strings.TrimSuffix(last[6], "."))
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("parse agy log reset %q: %w", last[6], err)
	}
	reset := at.Add(d)
	return []Limit{{
		Name: "quota", Kind: LimitWindow, Scope: ScopeAccount, UsedPct: new(100.0),
		ResetsAt: &reset, Note: "quota reached (from agy log)",
	}}, at, nil
}
