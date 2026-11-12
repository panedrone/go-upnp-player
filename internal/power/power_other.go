//go:build !windows

// Package power keeps the computer from sleeping while music plays: the stream comes
// from this process, so a sleeping PC cuts the renderer off mid-track.
package power

// StayAwake: not done outside Windows yet.
func StayAwake() {}
