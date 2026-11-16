package services

import "time"

// ScanGone is how long the Scan switch goes without hearing a listed device before it drops it
// from the list (a renderer unplugged, 2026-11-16): five searches in a row, so one lost answer
// does not drop a device.
const ScanGone = 5 * time.Second

// GoneUDNs returns the listed devices last heard ScanGone or more before now. A device with no
// record is not gone: it has just come onto the list.
func GoneUDNs(listed []string, lastSeen map[string]time.Time, now time.Time) []string {
	var out []string
	for _, u := range listed {
		if at, ok := lastSeen[u]; ok && now.Sub(at) >= ScanGone {
			out = append(out, u)
		}
	}
	return out
}

// NewUDNs returns the devices of found that known does not have, in found's order.
func NewUDNs(known, found []string) []string {
	have := make(map[string]bool, len(known))
	for _, u := range known {
		have[u] = true
	}
	var out []string
	for _, u := range found {
		if !have[u] {
			have[u] = true
			out = append(out, u)
		}
	}
	return out
}
