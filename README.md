# gpac — Go Package/binary Installer

`gpac` installs a Go binary straight from a GitHub repository without requiring
a Go toolchain (or `git`) to already be present on the machine running it.

It tries, in order:

1. **Prebuilt release** — download the release asset matching your OS/arch
   from the repo's latest (or a pinned) GitHub release, extract it if needed,
   and install the binary.
2. **Build from source** — if no matching release asset exists, `gpac`
   downloads a temporary Go toolchain (cached locally), fetches the repo's
   source tarball via the GitHub API, and builds it — no local Go or `git`
   install required.

## Install

```sh
go build -o gpac ./cmd/gpac
# or
go install ./cmd/gpac
```

## Usage

```sh
gpac [flags] <owner/repo | github.com/owner/repo | repo-url>
```

Examples:

```sh
gpac junegunn/fzf                       # install latest release
gpac rakyll/hey                         # no release binaries -> builds from source
gpac owner/repo --version v1.2.3        # install a specific tag
gpac owner/repo --bin-name mytool       # install under a different name
gpac owner/repo --bin-dir /usr/local/bin
gpac --list                             # show gpac-managed binaries
```

## Flags

| Flag | Description |
|---|---|
| `--bin-dir` | Directory to install the binary into (default: `~/bin`) |
| `--bin-name` | Name of the installed binary (default: repo name) |
| `--version` | Release tag / ref to install (default: latest) |
| `--list` | List all gpac-managed binaries and exit |

## How it tracks installs

Every successful install is recorded in a manifest at
`$XDG_CONFIG_HOME/gpac/manifest.json` (`~/Library/Application Support/gpac`
on macOS), so `gpac --list` can show what it manages independent of anything
else in `--bin-dir`. Entries whose binary no longer exists on disk are pruned
automatically.

## Notes

- Only the GitHub host is supported for the repo argument.
- `GITHUB_TOKEN`, if set, is sent as a bearer token on GitHub API requests
  (useful to avoid rate limiting).
- Downloaded Go toolchains are cached under `~/.cache/gpac/go` (or the
  platform equivalent) so subsequent source builds don't re-download them.
