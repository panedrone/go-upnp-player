//go:build !windows

package main

// hasConsole: outside Windows the process keeps the terminal it was started from.
func hasConsole() bool { return true }
