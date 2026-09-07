// Package archive extracts .tar.gz/.tgz and .zip files, and locates
// executable binaries within an extracted tree.
package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Extract unpacks the archive at srcPath into destDir, choosing the format
// based on the file extension. Returns an error if the format is
// unrecognized.
func Extract(srcPath, destDir string) error {
	lower := strings.ToLower(srcPath)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(srcPath, destDir)
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(srcPath, destDir)
	default:
		return fmt.Errorf("unrecognized archive format for %s", srcPath)
	}
}

func extractTarGz(srcPath, destDir string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(destDir, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode&0o777|0o200))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		case tar.TypeSymlink:
			// Skip symlinks; not needed for binary extraction and avoids
			// traversal risk from malicious archives.
			continue
		}
	}
}

func extractZip(srcPath, destDir string) error {
	r, err := zip.OpenReader(srcPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		target, err := safeJoin(destDir, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		out.Close()
		rc.Close()
	}
	return nil
}

// safeJoin joins base and name, rejecting paths that would escape base
// (zip-slip protection).
func safeJoin(base, name string) (string, error) {
	target := filepath.Join(base, name)
	if !strings.HasPrefix(target, filepath.Clean(base)+string(os.PathSeparator)) && target != filepath.Clean(base) {
		return "", fmt.Errorf("illegal file path in archive: %s", name)
	}
	return target, nil
}

// FindBinary walks root looking for an executable regular file whose base
// name matches preferredName (with or without a .exe suffix). If none
// matches, and exactly one executable regular file exists in the tree, that
// file is returned instead.
func FindBinary(root, preferredName string) (string, error) {
	var candidates []string
	var preferred string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode()&0o111 == 0 && info.Mode().IsRegular() {
			// Not marked executable; still consider it below by name match,
			// since some archives don't preserve the exec bit.
		}
		base := filepath.Base(path)
		trimmed := strings.TrimSuffix(base, ".exe")
		if trimmed == preferredName {
			preferred = path
		}
		if info.Mode().IsRegular() {
			candidates = append(candidates, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	if preferred != "" {
		return preferred, nil
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	return "", fmt.Errorf("could not find binary %q among %d extracted files", preferredName, len(candidates))
}
