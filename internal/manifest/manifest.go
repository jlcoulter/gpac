// Package manifest tracks the set of binaries gpac has installed so they
// can be listed later, independent of what else lives in the bin dir.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Entry records a single gpac-managed binary installation.
type Entry struct {
	Name        string    `json:"name"`
	Repo        string    `json:"repo"`
	Ref         string    `json:"ref,omitempty"`
	Method      string    `json:"method"` // "release" or "source"
	Path        string    `json:"path"`
	SHA256      string    `json:"sha256,omitempty"`
	InstalledAt time.Time `json:"installed_at"`
}

type manifestFile struct {
	Entries []Entry `json:"entries"`
}

// Path returns the location of the manifest file, creating its parent
// directory if necessary.
func Path() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	dir := filepath.Join(base, "gpac")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "manifest.json"), nil
}

func load(path string) (manifestFile, error) {
	var mf manifestFile
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return mf, nil
	}
	if err != nil {
		return mf, err
	}
	if len(data) == 0 {
		return mf, nil
	}
	if err := json.Unmarshal(data, &mf); err != nil {
		return mf, err
	}
	return mf, nil
}

func save(path string, mf manifestFile) error {
	data, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Record upserts an entry keyed by its install Path.
func Record(e Entry) error {
	path, err := Path()
	if err != nil {
		return err
	}
	mf, err := load(path)
	if err != nil {
		return err
	}

	e.InstalledAt = time.Now().UTC()
	replaced := false
	for i, existing := range mf.Entries {
		if existing.Path == e.Path {
			mf.Entries[i] = e
			replaced = true
			break
		}
	}
	if !replaced {
		mf.Entries = append(mf.Entries, e)
	}
	return save(path, mf)
}

// List returns all recorded entries, sorted by name, pruning any whose
// binary no longer exists on disk.
func List() ([]Entry, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	mf, err := load(path)
	if err != nil {
		return nil, err
	}

	var live []Entry
	for _, e := range mf.Entries {
		if _, err := os.Stat(e.Path); err == nil {
			live = append(live, e)
		}
	}
	if len(live) != len(mf.Entries) {
		mf.Entries = live
		_ = save(path, mf) // best-effort prune of stale entries
	}

	sort.Slice(live, func(i, j int) bool { return live[i].Name < live[j].Name })
	return live, nil
}

// Remove deletes any manifest entries matching the provided key. The key
// is compared against entry Name, Repo, and Path. It returns the removed
// entries so callers can perform any filesystem cleanup required.
func Remove(key string) ([]Entry, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	mf, err := load(path)
	if err != nil {
		return nil, err
	}

	var kept []Entry
	var removed []Entry
	for _, e := range mf.Entries {
		if e.Name == key || e.Repo == key || e.Path == key {
			removed = append(removed, e)
		} else {
			kept = append(kept, e)
		}
	}
	if len(removed) == 0 {
		return nil, fmt.Errorf("no manifest entry matches %s", key)
	}
	mf.Entries = kept
	if err := save(path, mf); err != nil {
		return nil, err
	}
	return removed, nil
}
