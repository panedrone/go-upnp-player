package services

import (
	"path/filepath"
	"strconv"
	"strings"
)

// How this copy of the player is updated; the update check offers the matching action.
const (
	UpdateBySetup = "setup" // installed by the Windows setup: download the new setup
	UpdateByGo    = "go"    // installed by go install: run go install again
	UpdateByPage  = "page"  // the zip or a local build: open the release page
)

// Release is what the update check learns about the latest release.
type Release struct {
	Tag      string // "v1.1.15"
	PageURL  string
	SetupURL string // empty when the release has no setup asset
}

// UpdateOffer is the answer to "check for updates": the latest version and what to do.
type UpdateOffer struct {
	Current string `json:"current"`
	Latest  string `json:"latest"`
	Newer   bool   `json:"newer"`
	Kind    string `json:"kind"`
	// URL the Download/Release page action opens; empty for UpdateByGo
	URL string `json:"url,omitempty"`
	// Command is the go install command for UpdateByGo, shown to the user
	Command string `json:"command,omitempty"`
}

// UpdateKindOf tells how this copy was installed. installed: the setup's uninstaller lies
// next to the exe. A go install copy lives in the Go bin folder (GOBIN, or GOPATH\bin)
// and Go is at hand to build the next one. Anything else (the zip, a local build) gets
// the release page.
func UpdateKindOf(installed bool, exeDir, goBinDir string) string {
	switch {
	case installed:
		return UpdateBySetup
	case goBinDir != "" && sameDir(exeDir, goBinDir):
		return UpdateByGo
	default:
		return UpdateByPage
	}
}

func sameDir(a, b string) bool {
	clean := func(p string) string { return strings.TrimRight(filepath.Clean(p), `\/`) }
	return strings.EqualFold(clean(a), clean(b))
}

// OfferUpdate builds the answer from the running version and the latest release.
func OfferUpdate(current string, rel Release, kind, module, ldflags string) UpdateOffer {
	o := UpdateOffer{Current: current, Latest: strings.TrimPrefix(rel.Tag, "v"), Kind: kind}
	o.Newer = NewerVersion(current, rel.Tag)
	switch kind {
	case UpdateBySetup:
		o.URL = rel.SetupURL
		if o.URL == "" {
			o.URL = rel.PageURL
		}
	case UpdateByGo:
		o.Command = GoInstallCommand(module, rel.Tag, ldflags)
	default:
		o.URL = rel.PageURL
	}
	return o
}

// GoInstallCommand is the command that updates a go install copy, with the linker flags
// the running exe was built with (the Windows build without a console needs them).
func GoInstallCommand(module, tag, ldflags string) string {
	cmd := "go install "
	if ldflags != "" {
		cmd += `-ldflags "` + ldflags + `" `
	}
	return cmd + module + "@" + tag
}

// NewerVersion reports whether latest ("v1.1.15" or "1.1.15") is above current.
// A version that does not parse is never newer: no offer is better than a wrong one.
func NewerVersion(current, latest string) bool {
	c, ok1 := parseVersion(current)
	l, ok2 := parseVersion(latest)
	if !ok1 || !ok2 {
		return false
	}
	for i := range c {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
