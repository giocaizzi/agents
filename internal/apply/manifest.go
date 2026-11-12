package apply

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
)

const manifestVersion = 1

// fileEntry records what agents managed in one file on the last run.
type fileEntry struct {
	Keys  [][]string          `json:"keys"`
	Union map[string][]string `json:"union,omitempty"`
}

type manifest struct {
	Version int                  `json:"version"`
	Files   map[string]fileEntry `json:"files"`
}

func newManifest() *manifest {
	return &manifest{Version: manifestVersion, Files: map[string]fileEntry{}}
}

// loadManifest reads the manifest; a missing file or an unknown version gives an empty one.
func loadManifest(path string) (*manifest, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return newManifest(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	if m.Version != manifestVersion || m.Files == nil {
		return newManifest(), nil
	}
	return &m, nil
}

func (m *manifest) equal(o *manifest) bool {
	return reflect.DeepEqual(m, o)
}

func (m *manifest) clone() *manifest {
	data, _ := json.Marshal(m) //nolint:errchkjson // plain strings cannot fail
	var out manifest
	_ = json.Unmarshal(data, &out)
	if out.Files == nil {
		out.Files = map[string]fileEntry{}
	}
	return &out
}

func (m *manifest) save(path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create manifest dir: %w", err)
	}
	return writeAtomic(path, append(data, '\n'), 0o600)
}
