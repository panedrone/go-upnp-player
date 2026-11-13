package services

import "testing"

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		cur, latest string
		want        bool
	}{
		{"1.1.14", "v1.1.15", true},
		{"1.1.14", "v1.2.0", true},
		{"1.1.14", "v2.0.0", true},
		{"1.1.14", "v1.1.14", false},
		{"1.1.14", "v1.1.9", false}, // numbers, not strings: 9 < 14
		{"1.1.14", "v1.0.99", false},
		{"1.1.14", "nightly", false},
		{"", "v1.1.15", false},
		{"1.1.14", "v1.1.15-rc1", false},
	}
	for _, c := range cases {
		if got := NewerVersion(c.cur, c.latest); got != c.want {
			t.Errorf("NewerVersion(%q, %q) = %v", c.cur, c.latest, got)
		}
	}
}

func TestUpdateKindOf(t *testing.T) {
	if k := UpdateKindOf(true, `C:\x`, `C:\x`); k != UpdateBySetup {
		t.Errorf("installed: %s", k)
	}
	if k := UpdateKindOf(false, `C:\Users\u\go\bin`, `c:\users\u\go\bin\`); k != UpdateByGo {
		t.Errorf("go bin, other case and a trailing slash: %s", k)
	}
	if k := UpdateKindOf(false, `D:\apps\player`, `C:\Users\u\go\bin`); k != UpdateByPage {
		t.Errorf("zip: %s", k)
	}
	if k := UpdateKindOf(false, `D:\apps\player`, ""); k != UpdateByPage {
		t.Errorf("no Go: %s", k)
	}
}

func TestOfferUpdate(t *testing.T) {
	rel := Release{Tag: "v1.1.15", PageURL: "https://page", SetupURL: "https://setup.exe"}
	o := OfferUpdate("1.1.14", rel, UpdateBySetup, "m", "")
	if !o.Newer || o.Latest != "1.1.15" || o.URL != "https://setup.exe" {
		t.Errorf("setup: %+v", o)
	}
	if o := OfferUpdate("1.1.14", Release{Tag: "v1.1.15", PageURL: "https://page"}, UpdateBySetup, "m", ""); o.URL != "https://page" {
		t.Errorf("no setup asset: %+v", o)
	}
	o = OfferUpdate("1.1.14", rel, UpdateByGo, "example.com/p/cmd/p", "-s -w -H=windowsgui")
	if o.URL != "" || o.Command != `go install -ldflags "-s -w -H=windowsgui" example.com/p/cmd/p@v1.1.15` {
		t.Errorf("go: %+v", o)
	}
	if o := OfferUpdate("1.1.14", rel, UpdateByPage, "m", ""); o.URL != "https://page" || o.Command != "" {
		t.Errorf("page: %+v", o)
	}
}
