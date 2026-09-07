// Command gpac installs a prebuilt (or source-built) Go binary from a
// GitHub repository, without requiring a Go toolchain to already be
// installed on the machine running gpac.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gpac/internal/archive"
	"gpac/internal/githubapi"
	"gpac/internal/manifest"
	"gpac/internal/platform"
	"gpac/internal/repoparse"
	"gpac/internal/toolchain"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gpac: error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError("expected a subcommand: install, list, remove, or update")
	}

	switch args[0] {
	case "install":
		fs := flag.NewFlagSet("gpac install", flag.ContinueOnError)
		binDir := fs.String("bin-dir", defaultBinDir(), "directory to install the binary into")
		binName := fs.String("bin-name", "", "name of the installed binary (default: repo name)")
		ref := fs.String("version", "", "release tag / ref to install (default: latest)")
		fs.Usage = func() {
			fmt.Fprintln(os.Stderr, "Usage: gpac install [flags] <owner/repo | github.com/owner/repo | repo-url>")
			fs.PrintDefaults()
		}
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			fs.Usage()
			return fmt.Errorf("expected exactly one repository argument")
		}
		repo, err := repoparse.Parse(fs.Arg(0))
		if err != nil {
			return err
		}
		if *ref != "" {
			repo.Ref = *ref
		}
		name := *binName
		if name == "" {
			name = repo.Name
		}
		if err := os.MkdirAll(*binDir, 0o755); err != nil {
			return fmt.Errorf("creating bin dir %s: %w", *binDir, err)
		}
		outPath := filepath.Join(*binDir, name)
		if runtime.GOOS == "windows" {
			outPath += ".exe"
		}
		if _, err := installToPath(repo, name, outPath, ""); err != nil {
			return err
		}
		fmt.Printf("Installed %s to %s\n", repo, outPath)
		return nil

	case "list":
		fs := flag.NewFlagSet("gpac list", flag.ContinueOnError)
		fs.Usage = func() {
			fmt.Fprintln(os.Stderr, "Usage: gpac list")
			fs.PrintDefaults()
		}
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			fs.Usage()
			return fmt.Errorf("gpac list does not take positional arguments")
		}
		return listInstalled()

	case "remove":
		fs := flag.NewFlagSet("gpac remove", flag.ContinueOnError)
		fs.Usage = func() {
			fmt.Fprintln(os.Stderr, "Usage: gpac remove <name|repo|path>")
			fs.PrintDefaults()
		}
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			fs.Usage()
			return fmt.Errorf("expected exactly one argument to remove (name, repo or path)")
		}
		key := fs.Arg(0)
		removed, err := manifest.Remove(key)
		if err != nil {
			return err
		}
		for _, e := range removed {
			if err := os.Remove(e.Path); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "gpac: warning: failed to remove %s: %v\n", e.Path, err)
			} else {
				fmt.Printf("Removed %s (%s)\n", e.Name, e.Path)
			}
		}
		return nil

	case "update":
		fs := flag.NewFlagSet("gpac update", flag.ContinueOnError)
		version := fs.String("version", "", "release tag / ref to update to (default: latest)")
		fs.Usage = func() {
			fmt.Fprintln(os.Stderr, "Usage: gpac update [--version <ref>] <name|repo|path>")
			fs.PrintDefaults()
		}
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			fs.Usage()
			return fmt.Errorf("expected exactly one argument to update (name, repo or path)")
		}
		key := fs.Arg(0)
		entries, err := manifest.List()
		if err != nil {
			return err
		}
		var match *manifest.Entry
		for i := range entries {
			if entries[i].Name == key || entries[i].Repo == key || entries[i].Path == key {
				match = &entries[i]
				break
			}
		}
		if match == nil {
			return fmt.Errorf("no installed binary matches %s", key)
		}
		repo, err := repoparse.Parse(match.Repo)
		if err != nil {
			return err
		}
		if *version != "" {
			repo.Ref = *version
		}
		updated, err := installToPath(repo, match.Name, match.Path, match.SHA256)
		if err != nil {
			return err
		}
		if !updated {
			fmt.Printf("Already up to date: %s (%s)\n", match.Name, repo)
			return nil
		}
		fmt.Printf("Updated %s to %s\n", match.Name, repo)
		return nil

	default:
		fmt.Fprintln(os.Stderr, "Usage: gpac <install|list|remove|update> ...")
		return fmt.Errorf("unsupported subcommand: %s", args[0])
	}
}

func usageError(msg string) error {
	fmt.Fprintln(os.Stderr, "Usage: gpac <install|list|remove|update> ...")
	return fmt.Errorf("%s", msg)
}

func installToPath(repo repoparse.Repo, name, outPath, currentSHA string) (bool, error) {
	tmp, err := os.CreateTemp("", "gpac-install-*")
	if err != nil {
		return false, err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	method := "release"
	if err := installFromRelease(repo, tmpPath); err != nil {
		fmt.Fprintf(os.Stderr, "gpac: no prebuilt release binary available (%v); building from source instead...\n", err)
		if err := installFromSource(repo, tmpPath); err != nil {
			return false, fmt.Errorf("building from source failed: %w", err)
		}
		method = "source"
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return false, err
	}

	newSHA, err := sha256File(tmpPath)
	if err != nil {
		return false, err
	}
	if currentSHA != "" && currentSHA == newSHA {
		return false, nil
	}
	if err := copyFile(tmpPath, outPath); err != nil {
		return false, err
	}

	if err := manifest.Record(manifest.Entry{
		Name:   name,
		Repo:   repo.String(),
		Ref:    repo.Ref,
		Method: method,
		Path:   outPath,
		SHA256: newSHA,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "gpac: warning: failed to record install in manifest: %v\n", err)
	}
	return true, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// listInstalled prints every binary gpac has installed, as recorded in its
// manifest.
func listInstalled() error {
	entries, err := manifest.List()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("No gpac-managed binaries installed.")
		return nil
	}
	for _, e := range entries {
		ref := e.Ref
		if ref == "" {
			ref = "latest"
		}
		fmt.Printf("%-20s %-30s %-8s %-8s %s\n", e.Name, e.Repo, ref, e.Method, e.Path)
	}
	return nil
}

func defaultBinDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, "bin")
}

// installFromRelease looks for a GitHub release with an asset matching the
// current OS/arch, downloads it, and installs the binary at outPath.
func installFromRelease(repo repoparse.Repo, outPath string) error {
	var rel *githubapi.Release
	var err error
	if repo.Ref != "" {
		rel, err = githubapi.ReleaseByTag(repo.Owner, repo.Name, repo.Ref)
	} else {
		rel, err = githubapi.LatestRelease(repo.Owner, repo.Name)
	}
	if err != nil {
		return err
	}
	if rel == nil {
		return fmt.Errorf("no releases found for %s", repo)
	}

	asset, err := bestAsset(rel, repo.Name)
	if err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "gpac-dl-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	dlPath := filepath.Join(tmpDir, asset.Name)
	fmt.Printf("Downloading %s (%s)...\n", asset.Name, rel.TagName)
	if err := githubapi.DownloadTo(asset.BrowserDownloadURL, dlPath); err != nil {
		return err
	}

	lower := strings.ToLower(asset.Name)
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".zip") {
		extractDir := filepath.Join(tmpDir, "extracted")
		if err := archive.Extract(dlPath, extractDir); err != nil {
			return err
		}
		binPath, err := archive.FindBinary(extractDir, repo.Name)
		if err != nil {
			return err
		}
		return copyFile(binPath, outPath)
	}

	// Asset is already a raw binary.
	return copyFile(dlPath, outPath)
}

// bestAsset scores every asset in the release against the current
// OS/arch and returns the best match.
func bestAsset(rel *githubapi.Release, repoName string) (githubapi.Asset, error) {
	type scored struct {
		asset githubapi.Asset
		score int
	}
	var candidates []scored
	for _, a := range rel.Assets {
		s := platform.ScoreAssetName(a.Name, runtime.GOOS, runtime.GOARCH)
		if s < 0 {
			continue
		}
		if strings.Contains(strings.ToLower(a.Name), strings.ToLower(repoName)) {
			s += 2
		}
		candidates = append(candidates, scored{a, s})
	}
	if len(candidates) == 0 {
		return githubapi.Asset{}, fmt.Errorf("no release asset matches %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	return candidates[0].asset, nil
}

// installFromSource bootstraps a temporary Go toolchain and builds the
// package directly from the repo's source tarball.
func installFromSource(repo repoparse.Repo, outPath string) error {
	goBin, goroot, err := toolchain.Ensure()
	if err != nil {
		return err
	}
	return toolchain.BuildFromSource(goBin, goroot, repo.Owner, repo.Name, repo.Ref, outPath)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := out.ReadFrom(in); err != nil {
		return err
	}
	return nil
}
