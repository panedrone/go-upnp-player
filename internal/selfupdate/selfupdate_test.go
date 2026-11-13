package selfupdate

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func swap[T any](t *testing.T, p *T, v T) {
	old := *p
	*p = v
	t.Cleanup(func() { *p = old })
}

func TestLatest(t *testing.T) {
	body := `{"tag_name":"v1.1.15","html_url":"https://github.com/x/releases/tag/v1.1.15",
		"assets":[{"name":"go-upnp-player-windows-amd64.zip","browser_download_url":"https://z"},
		{"name":"go-upnp-player-setup.exe","browser_download_url":"https://s"}]}`
	swap(t, &httpDo, func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("GitHub's API needs a User-Agent")
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	rel, err := Latest(context.Background())
	if err != nil || rel.Tag != "v1.1.15" || rel.SetupURL != "https://s" || !strings.HasSuffix(rel.PageURL, "v1.1.15") {
		t.Fatalf("%+v %v", rel, err)
	}

	swap(t, &httpDo, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Status: "403 Forbidden", Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	if _, err := Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("rate limit: %v", err)
	}
}

func TestGoBinDir(t *testing.T) {
	swap(t, &runGo, func(context.Context, ...string) ([]byte, error) { return []byte("\r\nC:\\Users\\u\\go\r\n"), nil })
	if got := GoBinDir(context.Background()); got != filepath.Join(`C:\Users\u\go`, "bin") {
		t.Errorf("GOPATH: %q", got)
	}
	swap(t, &runGo, func(context.Context, ...string) ([]byte, error) { return []byte("/opt/gobin\n/home/u/go\n"), nil })
	if got := GoBinDir(context.Background()); got != "/opt/gobin" {
		t.Errorf("GOBIN: %q", got)
	}
	swap(t, &runGo, func(context.Context, ...string) ([]byte, error) { return nil, errors.New("not found") })
	if got := GoBinDir(context.Background()); got != "" {
		t.Errorf("no Go: %q", got)
	}
}

func TestGoInstall(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "go-upnp-player.exe")
	if err := os.WriteFile(exe, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	swap(t, &executable, func() (string, error) { return exe, nil })

	// a failed build puts the running exe back
	swap(t, &runGo, func(context.Context, ...string) ([]byte, error) {
		return []byte("no such version"), errors.New("exit 1")
	})
	if err := GoInstall(context.Background(), "v9.9.9"); err == nil || !strings.Contains(err.Error(), "no such version") {
		t.Fatalf("error %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatal("the running exe was not put back")
	}

	// a build writes the new exe; the old one waits aside for the next start
	var got []string
	swap(t, &runGo, func(_ context.Context, args ...string) ([]byte, error) {
		got = args
		return nil, os.WriteFile(exe, []byte("new"), 0o644)
	})
	if err := GoInstall(context.Background(), "v1.1.15"); err != nil {
		t.Fatal(err)
	}
	if got[0] != "install" || got[len(got)-1] != Module+"@v1.1.15" {
		t.Fatalf("args %q", got)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Fatal("no new exe")
	}
	RemoveLeftover()
	if _, err := os.Stat(exe + replacedSuffix); !os.IsNotExist(err) {
		t.Fatal("the old exe was not removed")
	}
}
