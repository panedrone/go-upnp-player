package services

import (
	"reflect"
	"testing"
	"time"
)

func TestGoneUDNs(t *testing.T) {
	now := time.Date(2026, 11, 16, 22, 0, 0, 0, time.UTC)
	seen := map[string]time.Time{"a": now.Add(-ScanGone), "b": now.Add(-ScanGone + time.Second)}
	got := GoneUDNs([]string{"a", "b", "c"}, seen, now)
	if want := []string{"a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v (b heard 4 s ago, c has no record)", got, want)
	}
}

func TestNewUDNs(t *testing.T) {
	got := NewUDNs([]string{"a", "b"}, []string{"b", "c", "a", "d", "c"})
	if want := []string{"c", "d"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := NewUDNs([]string{"a"}, []string{"a"}); got != nil {
		t.Errorf("nothing new: got %v", got)
	}
}
