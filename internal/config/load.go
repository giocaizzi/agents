package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Options are the inputs of Load. Everything the loader touches is injected.
type Options struct {
	// Getenv reads the environment. Required.
	Getenv func(string) string
	// GOOS selects the os overlay and the platform directories (default runtime.GOOS).
	GOOS string
	// Home is the user home (default os.UserHomeDir).
	Home string
	// ConfigPath is an explicit user config file; it must exist.
	ConfigPath string
	// Profile is the -profile flag.
	Profile string
	// Harnesses is the -harnesses flag.
	Harnesses []string
	// Stat defaults to os.Stat.
	Stat func(string) (fs.FileInfo, error)
	// ReadFile defaults to os.ReadFile.
	ReadFile func(string) ([]byte, error)
}

// Resolved is the result of Load.
type Resolved struct {
	Config            Config
	Profile           string
	Harnesses         []Harness
	HarnessesDetected bool
	// Files are the config files read, lowest precedence first.
	Files []string
	// Origin maps every dotted leaf path of Config to the layer that set it.
	Origin map[string]string
	Dirs   Dirs
}

type loader struct {
	opts   Options
	merged map[string]any
	origin map[string]string
	files  []string
}

// Load resolves the configuration. Precedence, lowest first: defaults, user
// config, os overlay, selected profile overlay, env, flags.
// Tables merge recursively; scalars and arrays replace.
func Load(opts Options) (*Resolved, error) {
	if opts.Getenv == nil {
		return nil, errors.New("config: Options.Getenv is required")
	}
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if opts.Home == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("find home: %w", err)
		}
		opts.Home = home
	}
	if opts.Stat == nil {
		opts.Stat = os.Stat
	}
	if opts.ReadFile == nil {
		opts.ReadFile = os.ReadFile
	}
	dirs := ResolveDirs(opts.Getenv, opts.GOOS, opts.Home)
	l := &loader{opts: opts, merged: map[string]any{}, origin: map[string]string{}}

	l.apply("default", map[string]any{
		"schema_version": int64(1),
		"instructions":   map[string]any{"dir": filepath.Join(dirs.Config, "instructions")},
	})
	if err := l.loadUser(dirs); err != nil {
		return nil, err
	}
	l.applyOS()
	profile, err := l.applyProfile()
	if err != nil {
		return nil, err
	}
	l.applyEnvAndFlags()

	cfg, err := decodeStrict(l.merged)
	if err != nil {
		return nil, fmt.Errorf("decode merged config: %w", err)
	}
	r := &Resolved{Config: cfg, Profile: profile, Files: l.files, Dirs: dirs, Origin: l.finalOrigin()}
	if len(cfg.Harnesses) > 0 {
		r.Harnesses = cfg.Harnesses
	} else {
		r.Harnesses = detect(opts)
		r.HarnessesDetected = true
		r.Origin["harnesses"] = "auto-detect"
	}
	return r, nil
}

func (l *loader) loadUser(dirs Dirs) error {
	path, explicit := l.opts.ConfigPath, true
	if path == "" {
		path = l.opts.Getenv("AGENTS_CONFIG")
	}
	if path == "" {
		path, explicit = filepath.Join(dirs.Config, "config.toml"), false
	}
	return l.loadFile(path, explicit)
}

func (l *loader) loadFile(path string, explicit bool) error {
	data, err := l.opts.ReadFile(path)
	if err != nil {
		if !explicit && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return decodeError(path, err)
	}
	var m map[string]any
	if err := toml.Unmarshal(data, &m); err != nil {
		return decodeError(path, err)
	}
	l.files = append(l.files, path)
	l.apply(path, m)
	return nil
}

// decodeError names the file, the key and the line of a strict decode failure.
func decodeError(path string, err error) error {
	if sm, ok := errors.AsType[*toml.StrictMissingError](err); ok {
		errs := make([]error, 0, len(sm.Errors))
		for i := range sm.Errors {
			row, _ := sm.Errors[i].Position()
			errs = append(errs, fmt.Errorf("%s:%d: unknown key %q", path, row, strings.Join(sm.Errors[i].Key(), ".")))
		}
		return errors.Join(errs...)
	}
	if de, ok := errors.AsType[*toml.DecodeError](err); ok {
		row, col := de.Position()
		return fmt.Errorf("%s:%d:%d: %s", path, row, col, strings.TrimPrefix(de.Error(), "toml: "))
	}
	return fmt.Errorf("%s: %w", path, err)
}

func (l *loader) applyOS() {
	osTable, _ := l.merged["os"].(map[string]any)
	ov, ok := osTable[l.opts.GOOS].(map[string]any)
	if !ok {
		return
	}
	l.apply("os "+l.opts.GOOS, overlay(ov))
}

// applyProfile picks the profile (flag, then env, then the merged key) and
// merges its overlay.
func (l *loader) applyProfile() (string, error) {
	name, _ := l.merged["profile"].(string)
	if v := l.opts.Getenv("AGENTS_PROFILE"); v != "" {
		name = v
	}
	if l.opts.Profile != "" {
		name = l.opts.Profile
	}
	if name == "" {
		return "", nil
	}
	profiles, _ := l.merged["profiles"].(map[string]any)
	p, ok := profiles[name].(map[string]any)
	if !ok {
		return "", fmt.Errorf("unknown profile %q (declared: %s)", name, strings.Join(slices.Sorted(maps.Keys(profiles)), ", "))
	}
	l.apply("profile "+name, overlay(p))
	return name, nil
}

func (l *loader) applyEnvAndFlags() {
	if v := l.opts.Getenv("AGENTS_PROFILE"); v != "" {
		l.apply("env AGENTS_PROFILE", map[string]any{"profile": v})
	}
	if v := l.opts.Getenv("AGENTS_HARNESSES"); v != "" {
		l.apply("env AGENTS_HARNESSES", map[string]any{"harnesses": splitList(v)})
	}
	if l.opts.Profile != "" {
		l.apply("flag -profile", map[string]any{"profile": l.opts.Profile})
	}
	if len(l.opts.Harnesses) > 0 {
		l.apply("flag -harnesses", map[string]any{"harnesses": splitList(strings.Join(l.opts.Harnesses, ","))})
	}
}

func splitList(s string) []any {
	var out []any
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// overlay copies a profile or os table and drops the composition data: the
// top-level instructions list and every harness.<h>.instructions list.
func overlay(src map[string]any) map[string]any {
	out := deepCopy(src)
	delete(out, "instructions")
	if h, ok := out["harness"].(map[string]any); ok {
		for _, t := range h {
			if tm, ok := t.(map[string]any); ok {
				delete(tm, "instructions")
			}
		}
	}
	return out
}

// apply merges m into the result and records label as the origin of its leaves.
func (l *loader) apply(label string, m map[string]any) {
	merge(l.merged, m)
	for _, p := range leaves(m, "") {
		l.origin[p] = label
	}
}

// finalOrigin keeps the origins of the leaves that survived the merge.
func (l *loader) finalOrigin() map[string]string {
	out := map[string]string{}
	for _, p := range leaves(l.merged, "") {
		out[p] = l.origin[p]
	}
	return out
}

// merge merges src into dst: tables recursively, everything else replaces.
func merge(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				merge(dm, sm)
				continue
			}
		}
		dst[k] = deepCopyValue(v)
	}
}

func deepCopy(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = deepCopyValue(v)
	}
	return out
}

func deepCopyValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return deepCopy(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = deepCopyValue(e)
		}
		return out
	default:
		return v
	}
}

// leaves lists the dotted paths of every non-table value, sorted.
func leaves(m map[string]any, prefix string) []string {
	var out []string
	for _, k := range slices.Sorted(maps.Keys(m)) {
		p := prefix + k
		if sub, ok := m[k].(map[string]any); ok {
			out = append(out, leaves(sub, p+".")...)
			continue
		}
		out = append(out, p)
	}
	return out
}

func decodeStrict(m map[string]any) (Config, error) {
	var cfg Config
	b, err := toml.Marshal(m)
	if err != nil {
		return cfg, fmt.Errorf("encode: %w", err)
	}
	dec := toml.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("decode: %w", err)
	}
	return cfg, nil
}

// detect selects the harnesses whose home exists.
func detect(opts Options) []Harness {
	homes := map[Harness]string{
		Claude:      filepath.Join(opts.Home, ".claude"),
		Copilot:     filepath.Join(opts.Home, ".copilot"),
		VSCode:      vscodeUserDir(opts),
		Codex:       filepath.Join(opts.Home, ".codex"),
		OpenCode:    filepath.Join(opts.Home, ".config", "opencode"),
		Pi:          filepath.Join(opts.Home, ".pi"),
		Antigravity: filepath.Join(opts.Home, ".gemini", "antigravity-cli"),
	}
	var out []Harness
	for _, h := range All() {
		if _, err := opts.Stat(homes[h]); err == nil {
			out = append(out, h)
		}
	}
	return out
}

func vscodeUserDir(opts Options) string {
	switch opts.GOOS {
	case "darwin":
		return filepath.Join(opts.Home, "Library", "Application Support", "Code", "User")
	case "windows":
		appdata := opts.Getenv("APPDATA")
		if appdata == "" {
			appdata = filepath.Join(opts.Home, "AppData", "Roaming")
		}
		return filepath.Join(appdata, "Code", "User")
	default:
		return filepath.Join(opts.Home, ".config", "Code", "User")
	}
}
