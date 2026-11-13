//go:build !windows

// Package window shows the UI in a window of its own on Windows; elsewhere the player
// always uses the browser.
package window

// Run: no window outside Windows; the caller opens the browser.
func Run(url, title string) bool { return false }

// Close: there is never a window to close.
func Close() {}

// Show: there is never a window to show.
func Show() bool { return false }

// AllowForeground: nothing to allow.
func AllowForeground() {}
