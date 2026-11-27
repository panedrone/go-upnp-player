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
	if k := UpdateKindOf(true, true); k != UpdateBySetup {
		t.Errorf("installed: %s", k)
	}
	if k := UpdateKindOf(false, true); k != UpdateByGo {
		t.Errorf("any other copy with Go: %s", k)
	}
	if k := UpdateKindOf(false, false); k != UpdateByPage {
		t.Errorf("no Go: %s", k)
	}
}

func TestOfferUpdate(t *testing.T) {
	rel := Release{Tag: "v1.1.15", PageURL: "https://page", SetupURL: "https://setup.exe"}
	o := OfferUpdate("1.1.14", rel, UpdateBySetup)
	if !o.Newer || o.Latest != "1.1.15" || o.URL != "https://setup.exe" {
		t.Errorf("setup: %+v", o)
	}
	if o := OfferUpdate("1.1.14", Release{Tag: "v1.1.15", PageURL: "https://page"}, UpdateBySetup); o.URL != "https://page" {
		t.Errorf("no setup asset: %+v", o)
	}
	o = OfferUpdate("1.1.14", rel, UpdateByGo)
	if !o.Newer || o.URL != "" {
		t.Errorf("go: %+v", o)
	}
	if o := OfferUpdate("1.1.14", rel, UpdateByPage); o.URL != "https://page" {
		t.Errorf("page: %+v", o)
	}
}
