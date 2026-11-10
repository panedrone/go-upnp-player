//go:build !windows

package server

// HasConsole: outside Windows the process keeps the terminal it was started from.
func HasConsole() bool { return true }
