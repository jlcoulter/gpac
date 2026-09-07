// Package platform provides helpers for matching release assets and Go
// toolchain downloads to the current operating system and architecture.
package platform

import "strings"

// OSAliases maps runtime.GOOS to alternate names commonly used in release
// asset filenames.
func OSAliases(goos string) []string {
	switch goos {
	case "darwin":
		return []string{"darwin", "macos", "mac", "osx"}
	case "linux":
		return []string{"linux"}
	case "windows":
		return []string{"windows", "win"}
	default:
		return []string{goos}
	}
}

// ArchAliases maps runtime.GOARCH to alternate names commonly used in
// release asset filenames.
func ArchAliases(goarch string) []string {
	switch goarch {
	case "amd64":
		return []string{"amd64", "x86_64", "x64"}
	case "arm64":
		return []string{"arm64", "aarch64"}
	case "386":
		return []string{"386", "i386", "x86"}
	case "arm":
		return []string{"arm", "armv7", "armv6"}
	default:
		return []string{goarch}
	}
}

// ScoreAssetName returns a match score for a release asset filename against
// the given goos/goarch, or -1 if it doesn't look like a match. Higher is
// better.
func ScoreAssetName(name, goos, goarch string) int {
	lower := strings.ToLower(name)

	// Never consider checksum/signature/metadata files as installable assets.
	for _, bad := range []string{".sha256", ".sig", ".asc", "checksums", "checksum", ".sbom", ".sbom.json", ".txt", ".pem"} {
		if strings.Contains(lower, bad) {
			return -1
		}
	}

	osScore := -1
	for i, alias := range OSAliases(goos) {
		if strings.Contains(lower, alias) {
			osScore = 10 - i
			break
		}
	}
	if osScore < 0 {
		return -1
	}

	archScore := -1
	for i, alias := range ArchAliases(goarch) {
		if strings.Contains(lower, alias) {
			archScore = 10 - i
			break
		}
	}
	if archScore < 0 {
		return -1
	}

	score := osScore + archScore
	switch {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		score += 5
	case strings.HasSuffix(lower, ".zip"):
		score += 4
	case !strings.Contains(lower, "."):
		// Likely a raw, extension-less binary.
		score += 3
	}
	return score
}

// GoDownloadArch maps runtime.GOARCH to the arch name used in official Go
// toolchain download filenames (they already match GOARCH in practice, but
// this keeps the mapping centralized).
func GoDownloadArch(goarch string) string {
	return goarch
}
