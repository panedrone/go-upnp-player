package library

import (
	"runtime"
	"strings"
)

// foldCase: paths differing only in case name the same file. True where the file system is
// case-insensitive as a rule (Windows, macOS); on Linux "Covers" and "covers" are two folders.
var foldCase = runtime.GOOS == "windows" || runtime.GOOS == "darwin"

// pathKey is a path as a map key or for comparison: folded to lower case only where the
// file system folds it too.
func pathKey(p string) string {
	if foldCase {
		return strings.ToLower(p)
	}
	return p
}
