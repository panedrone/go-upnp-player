// Package selfupdate asks GitHub for the latest release and updates a go install copy of
// the player. Nothing here runs by itself: only the user's "Check for updates" and
// "Update" clicks reach it.
package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/panedrone/go-upnp-player/internal/services"
)

// Module is what go install builds.
const Module = "github.com/panedrone/go-upnp-player/cmd/go-upnp-player"

const latestURL = "https://api.github.com/repos/panedrone/go-upnp-player/releases/latest"

// setupAsset is the installer's file name in a release.
const setupAsset = "go-upnp-player-setup.exe"

// Replaceable in tests.
var (
	httpDo = (&http.Client{Timeout: 10 * time.Second}).Do
	// runGo runs the go tool with the given arguments and returns its combined output
	runGo = func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "go", args...)
		hideWindow(cmd)
		return cmd.CombinedOutput()
	}
	executable = os.Executable
)

// Latest asks GitHub for the latest release.
func Latest(ctx context.Context) (services.Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return services.Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "go-upnp-player")
	resp, err := httpDo(req)
	if err != nil {
		return services.Release{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return services.Release{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var r struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return services.Release{}, fmt.Errorf("GitHub's answer: %w", err)
	}
	rel := services.Release{Tag: r.TagName, PageURL: r.HTMLURL}
	for _, a := range r.Assets {
		if a.Name == setupAsset {
			rel.SetupURL = a.URL
		}
	}
	return rel, nil
}

// GoBinDir is where go install puts programs (GOBIN, else GOPATH\bin); empty without Go.
func GoBinDir(ctx context.Context) string {
	out, err := runGo(ctx, "env", "GOBIN", "GOPATH")
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(string(out), "\r", ""), "\n")
	if len(lines) >= 1 && strings.TrimSpace(lines[0]) != "" {
		return strings.TrimSpace(lines[0])
	}
	if len(lines) >= 2 && strings.TrimSpace(lines[1]) != "" {
		gopath := strings.Split(strings.TrimSpace(lines[1]), string(os.PathListSeparator))[0]
		return filepath.Join(gopath, "bin")
	}
	return ""
}

// ExeDir is the folder of the running exe.
func ExeDir() string {
	exe, err := executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// LdFlags are the linker flags the running exe was built with, so the update is built
// the same way (the Windows build without a console needs -H=windowsgui).
func LdFlags() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range bi.Settings {
		if s.Key == "-ldflags" {
			return s.Value
		}
	}
	return ""
}

// replacedSuffix marks the previous exe moved aside during an update.
const replacedSuffix = ".replaced"

// GoInstall builds tag with go install over the running exe. Windows keeps a running exe
// locked against overwriting but lets it be renamed, so it is moved aside first and put
// back if the build fails; the leftover is removed on the next start (RemoveLeftover).
func GoInstall(ctx context.Context, tag string) error {
	exe, err := executable()
	if err != nil {
		return err
	}
	aside := exe + replacedSuffix
	_ = os.Remove(aside)
	if err := os.Rename(exe, aside); err != nil {
		return fmt.Errorf("move the running exe aside: %w", err)
	}
	args := []string{"install"}
	if f := LdFlags(); f != "" {
		args = append(args, "-ldflags", f)
	}
	args = append(args, Module+"@"+tag)
	if out, err := runGo(ctx, args...); err != nil {
		_ = os.Rename(aside, exe)
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New("go install: " + msg)
	}
	if _, err := os.Stat(exe); err != nil {
		_ = os.Rename(aside, exe)
		return fmt.Errorf("go install left no %s", filepath.Base(exe))
	}
	return nil
}

// Restart starts the freshly installed exe; it takes the port over from this process the
// way an update always does (ADR-012), and the open page reloads itself.
func Restart(args []string) error {
	exe, err := executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, append(append([]string(nil), args...), "-no-browser")...)
	return cmd.Start()
}

// RemoveLeftover deletes the exe an earlier update moved aside.
func RemoveLeftover() {
	if exe, err := executable(); err == nil {
		_ = os.Remove(exe + replacedSuffix)
	}
}

// the answer to the last check: Open and Install act only on what it offered, never on
// a URL or version sent by the page
var (
	offerMu sync.Mutex
	offer   services.UpdateOffer
	offered services.Release
)

// Check asks GitHub for the latest release and tells what this copy can do about it.
func Check(ctx context.Context, current string, installed bool) (services.UpdateOffer, error) {
	rel, err := Latest(ctx)
	if err != nil {
		return services.UpdateOffer{}, err
	}
	goBin := ""
	if !installed {
		goBin = GoBinDir(ctx)
	}
	o := services.OfferUpdate(current, rel, services.UpdateKindOf(installed, ExeDir(), goBin), Module, LdFlags())
	offerMu.Lock()
	offer, offered = o, rel
	offerMu.Unlock()
	return o, nil
}

// OfferedURL is the download or release page the last check offered; empty if none.
func OfferedURL() string {
	offerMu.Lock()
	defer offerMu.Unlock()
	if !offer.Newer {
		return ""
	}
	return offer.URL
}

// InstallOffered runs go install for the version the last check offered.
func InstallOffered(ctx context.Context) error {
	offerMu.Lock()
	o, tag := offer, offered.Tag
	offerMu.Unlock()
	if !o.Newer || o.Kind != services.UpdateByGo {
		return errors.New("no go install update was offered: check for updates first")
	}
	return GoInstall(ctx, tag)
}
