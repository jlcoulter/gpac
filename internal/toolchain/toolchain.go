// Package toolchain bootstraps a temporary Go toolchain (no local Go
// installation required) and uses it to build a package from source.
package toolchain

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gpac/internal/archive"
	"gpac/internal/githubapi"
)

const dlIndexURL = "https://go.dev/dl/?mode=json"

type goFile struct {
	Filename string `json:"filename"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Kind     string `json:"kind"`
}

type goVersion struct {
	Version string   `json:"version"`
	Stable  bool     `json:"stable"`
	Files   []goFile `json:"files"`
}

// CacheDir returns the directory gpac uses to cache downloaded Go
// toolchains, creating it if necessary.
func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", err
		}
		base = filepath.Join(home, ".cache")
	}
	dir := filepath.Join(base, "gpac", "go")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// Ensure downloads and extracts the latest stable Go toolchain for the
// current OS/arch into the gpac cache (skipping the download if already
// present), and returns the path to its "go" binary and GOROOT.
func Ensure() (goBin string, goroot string, err error) {
	cacheDir, err := CacheDir()
	if err != nil {
		return "", "", err
	}

	version, file, err := latestToolchain(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", "", err
	}

	goroot = filepath.Join(cacheDir, version)
	binName := "go"
	if runtime.GOOS == "windows" {
		binName = "go.exe"
	}
	goBin = filepath.Join(goroot, "go", "bin", binName)

	if _, statErr := os.Stat(goBin); statErr == nil {
		return goBin, filepath.Join(goroot, "go"), nil
	}

	tmpArchive, err := os.CreateTemp("", "gpac-go-*."+archiveExt(file.Filename))
	if err != nil {
		return "", "", err
	}
	tmpArchivePath := tmpArchive.Name()
	tmpArchive.Close()
	defer os.Remove(tmpArchivePath)

	downloadURL := "https://go.dev/dl/" + file.Filename
	fmt.Printf("Downloading Go toolchain %s (%s)...\n", version, downloadURL)
	if err := githubapi.DownloadTo(downloadURL, tmpArchivePath); err != nil {
		return "", "", fmt.Errorf("downloading Go toolchain: %w", err)
	}

	if err := os.MkdirAll(goroot, 0o755); err != nil {
		return "", "", err
	}
	if err := archive.Extract(tmpArchivePath, goroot); err != nil {
		return "", "", fmt.Errorf("extracting Go toolchain: %w", err)
	}

	return goBin, filepath.Join(goroot, "go"), nil
}

func archiveExt(filename string) string {
	if strings.HasSuffix(filename, ".tar.gz") {
		return "tar.gz"
	}
	return "zip"
}

func latestToolchain(goos, goarch string) (version string, file goFile, err error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(dlIndexURL)
	if err != nil {
		return "", goFile{}, fmt.Errorf("fetching Go release index: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", goFile{}, fmt.Errorf("fetching Go release index: unexpected status %s", resp.Status)
	}

	var versions []goVersion
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return "", goFile{}, fmt.Errorf("parsing Go release index: %w", err)
	}

	for _, v := range versions {
		if !v.Stable {
			continue
		}
		for _, f := range v.Files {
			if f.Kind == "archive" && f.OS == goos && f.Arch == goarch {
				return v.Version, f, nil
			}
		}
	}
	return "", goFile{}, fmt.Errorf("no stable Go toolchain found for %s/%s", goos, goarch)
}

// BuildFromSource downloads the source tarball for owner/repo at ref (empty
// = default branch), builds it with the bootstrapped goBin, and writes the
// resulting binary to outPath.
func BuildFromSource(goBin, goroot, owner, repo, ref, outPath string) error {
	workDir, err := os.MkdirTemp("", "gpac-src-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	tarPath := filepath.Join(workDir, "src.tar.gz")
	url := githubapi.SourceTarballURL(owner, repo, ref)
	fmt.Printf("Downloading source from %s...\n", url)
	if err := githubapi.DownloadTo(url, tarPath); err != nil {
		return fmt.Errorf("downloading source tarball: %w", err)
	}

	srcRoot := filepath.Join(workDir, "src")
	if err := os.MkdirAll(srcRoot, 0o755); err != nil {
		return err
	}
	if err := archive.Extract(tarPath, srcRoot); err != nil {
		return fmt.Errorf("extracting source tarball: %w", err)
	}

	repoDir, err := singleSubdir(srcRoot)
	if err != nil {
		return err
	}

	buildCache := filepath.Join(workDir, "gocache")
	gopath := filepath.Join(workDir, "gopath")
	env := append(os.Environ(),
		"GOROOT="+goroot,
		"GOCACHE="+buildCache,
		"GOPATH="+gopath,
		"GOTOOLCHAIN=auto",
		"GOFLAGS=",
	)

	// Resolve module dependencies and populate go.sum before building, so
	// the build works even when the source tarball ships without a complete
	// go.sum (e.g. for users without a local Go installation). go mod tidy
	// adds any missing go.sum entries (go mod download alone does not), which
	// is required for the subsequent go build to succeed.
	tidyCmd := exec.Command(goBin, "mod", "tidy")
	tidyCmd.Dir = repoDir
	tidyCmd.Env = env
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tidying module dependencies: %w\n%s", err, out)
	}

	candidates := []string{".", "./cmd/" + repo}
	var lastErr error
	for _, pkg := range candidates {
		if pkg != "." {
			if _, statErr := os.Stat(filepath.Join(repoDir, "cmd", repo)); statErr != nil {
				continue
			}
		}
		cmd := exec.Command(goBin, "build", "-o", outPath, pkg)
		cmd.Dir = repoDir
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		lastErr = fmt.Errorf("go build %s: %w\n%s", pkg, err, out)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no buildable Go package found in %s", repoDir)
	}
	return lastErr
}

// singleSubdir returns the single directory entry under dir (GitHub source
// tarballs extract to exactly one top-level "owner-repo-sha" directory).
func singleSubdir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) != 1 {
		return "", fmt.Errorf("expected exactly one top-level directory in extracted source, found %d", len(dirs))
	}
	return filepath.Join(dir, dirs[0]), nil
}
