package config

import (
	"os"
	"path/filepath"
	"testing"
)

func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, k := range []string{"APPDATA", "XDG_CONFIG_HOME", "HOME"} {
		t.Setenv(k, dir)
	}
	cur = Config{}
	t.Cleanup(func() { cur = Config{} })
	return filepath.Dir(path())
}

func write(t *testing.T, p, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMissingFileIsEmptyConfig(t *testing.T) {
	isolate(t)
	c, err := Load()
	if err != nil || c.Renderer != "" {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestUpdateSurvivesReload(t *testing.T) {
	isolate(t)
	if err := Update(func(c *Config) {
		c.Roots = []string{"/music"}
		c.Renderer = "uuid:1"
		c.Playlist = Playlist{IDs: []string{"a"}, Index: 0}
	}); err != nil {
		t.Fatal(err)
	}
	cur = Config{}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Renderer != "uuid:1" || len(c.Roots) != 1 || c.Playlist.IDs[0] != "a" {
		t.Fatalf("%+v", c)
	}
}

func TestLoadFallsBackToTheNameBeforeTheRename(t *testing.T) {
	isolate(t)
	write(t, legacyPath(), `{"renderer":"old"}`)
	c, err := Load()
	if err != nil || c.Renderer != "old" {
		t.Fatalf("%+v %v", c, err)
	}
	write(t, path(), `{"renderer":"new"}`)
	if c, err = Load(); err != nil || c.Renderer != "new" {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestLoadCorruptFile(t *testing.T) {
	isolate(t)
	write(t, path(), "{broken")
	if _, err := Load(); err == nil {
		t.Fatal("corrupt file accepted")
	}
}

func TestLoadReadError(t *testing.T) {
	isolate(t)
	if err := os.MkdirAll(path(), 0o755); err != nil { // a directory where the file should be
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("want a read error")
	}
	if err := Update(func(*Config) {}); err == nil {
		t.Fatal("want a write error")
	}
}

func TestGetReturnsACopyOfRoots(t *testing.T) {
	isolate(t)
	cur.Roots = []string{"/a"}
	Get().Roots[0] = "/changed"
	if cur.Roots[0] != "/a" {
		t.Fatal("Get leaked the internal slice")
	}
}
