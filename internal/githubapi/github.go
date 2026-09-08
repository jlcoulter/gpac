// Package githubapi provides a minimal client for the parts of the GitHub
// REST API needed to find release assets and source tarballs, using only
// the standard library.
package githubapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

const apiBase = "https://api.github.com"

// Asset is a single downloadable file attached to a release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// Release is a GitHub release, trimmed to the fields we need.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

// downloadClient has a much longer timeout since it's used to stream
// potentially large files (release archives, Go toolchains).
var downloadClient = &http.Client{Timeout: 15 * time.Minute}

func newRequest(method, url string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gpac-installer")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	return req, nil
}

func getJSON(url string, out interface{}) error {
	req, err := newRequest(http.MethodGet, url)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request to %s failed: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// LatestRelease fetches the latest published release for owner/repo.
// It returns (nil, nil) if the repository has no releases (404).
func LatestRelease(owner, repo string) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", apiBase, owner, repo)
	return getRelease(url)
}

// ReleaseByTag fetches a specific release by tag name.
func ReleaseByTag(owner, repo, tag string) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", apiBase, owner, repo, url.PathEscape(tag))
	return getRelease(url)
}

func getRelease(url string) (*Release, error) {
	req, err := newRequest(http.MethodGet, url)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}
	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// DownloadTo streams the given URL to a local file path, following the
// redirects GitHub uses for both release assets and source tarballs.
func DownloadTo(url, destPath string) error {
	req, err := newRequest(http.MethodGet, url)
	if err != nil {
		return err
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s failed: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: unexpected status %s", url, resp.Status)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("writing %s: %w", destPath, err)
	}
	return nil
}

// SourceTarballURL returns the URL for a repo's source tarball at the given
// ref ("" means the default branch).
func SourceTarballURL(owner, repo, ref string) string {
	if ref == "" {
		return fmt.Sprintf("%s/repos/%s/%s/tarball", apiBase, owner, repo)
	}
	return fmt.Sprintf("%s/repos/%s/%s/tarball/%s", apiBase, owner, repo, url.PathEscape(ref))
}
