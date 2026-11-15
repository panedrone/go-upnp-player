package services

import (
	"slices"
	"testing"
)

func TestSetStarred(t *testing.T) {
	in := []string{"a", "b"}
	if got := SetStarred(in, "c", true); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("add: %v", got)
	}
	if got := SetStarred(in, "b", true); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("add twice: %v", got)
	}
	if got := SetStarred(in, "a", false); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("remove: %v", got)
	}
	if got := SetStarred(in, "x", false); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("remove missing: %v", got)
	}
	if !slices.Equal(in, []string{"a", "b"}) {
		t.Fatalf("input changed: %v", in)
	}
}

func TestStarredSet(t *testing.T) {
	set := StarredSet([]string{"a", "b"})
	if !set["a"] || !set["b"] || set["c"] {
		t.Fatalf("set: %v", set)
	}
}
