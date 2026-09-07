// Package repoparse normalizes the many ways a user might spell a GitHub
// repository remote (owner/repo, full URL, git+ssh, etc.) into an
// owner/repo pair.
package repoparse

import (
	"fmt"
	"regexp"
	"strings"
)

// Repo identifies a GitHub repository and, optionally, the ref (tag/branch)
// the user wants installed.
type Repo struct {
	Owner string
	Name  string
	Ref   string // optional, e.g. "v1.2.3"; empty means "latest"
}

var slugRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Parse accepts input such as:
//
//	owner/repo
//	owner/repo@v1.2.3
//	github.com/owner/repo
//	https://github.com/owner/repo
//	https://github.com/owner/repo.git
//	git@github.com:owner/repo.git
//
// and returns the parsed owner/repo/ref.
func Parse(remote string) (Repo, error) {
	s := strings.TrimSpace(remote)
	if s == "" {
		return Repo{}, fmt.Errorf("empty repository remote")
	}

	var ref string
	if idx := strings.LastIndex(s, "@"); idx > 0 && !strings.Contains(s[idx:], ":") && !strings.HasPrefix(s, "git@") {
		ref = s[idx+1:]
		s = s[:idx]
	}
	// Handle git@github.com:owner/repo.git separately since it also has "@".
	if strings.HasPrefix(s, "git@") {
		s = strings.TrimPrefix(s, "git@")
		s = strings.Replace(s, ":", "/", 1)
	}

	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "github.com/")
	s = strings.TrimSuffix(s, ".git")
	s = strings.Trim(s, "/")

	parts := strings.Split(s, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Repo{}, fmt.Errorf("could not parse %q as an owner/repo GitHub reference", remote)
	}
	owner, name := parts[0], parts[1]
	if !slugRe.MatchString(owner) || !slugRe.MatchString(name) {
		return Repo{}, fmt.Errorf("invalid owner/repo in %q", remote)
	}

	return Repo{Owner: owner, Name: name, Ref: ref}, nil
}

// String renders the repo back as "owner/repo".
func (r Repo) String() string {
	return r.Owner + "/" + r.Name
}
