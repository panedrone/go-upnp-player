package services

import (
	"errors"
	"strconv"
	"strings"
)

// ErrRangeNotSatisfiable means the range is outside the file (response 416).
var ErrRangeNotSatisfiable = errors.New("range not satisfiable")

// ParseRange parses the Range header for a file of the given size.
// A single range is supported: bytes=a-b, bytes=a-, bytes=-n.
// partial=false means the header is absent or not understood; serve the whole file.
// end is inclusive.
func ParseRange(header string, size int64) (start, end int64, partial bool, err error) {
	header = strings.TrimSpace(header)
	if header == "" || !strings.HasPrefix(header, "bytes=") {
		return 0, size - 1, false, nil
	}
	spec := strings.TrimPrefix(header, "bytes=")
	if strings.Contains(spec, ",") {
		// multiple ranges: renderers do not need them, serve the whole file
		return 0, size - 1, false, nil
	}
	a, b, ok := strings.Cut(strings.TrimSpace(spec), "-")
	if !ok {
		return 0, size - 1, false, nil
	}
	if a == "" {
		n, perr := strconv.ParseInt(b, 10, 64)
		if perr != nil || n <= 0 {
			return 0, 0, false, ErrRangeNotSatisfiable
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true, nil
	}
	s, perr := strconv.ParseInt(a, 10, 64)
	if perr != nil || s < 0 || s >= size {
		return 0, 0, false, ErrRangeNotSatisfiable
	}
	e := size - 1
	if b != "" {
		v, perr := strconv.ParseInt(b, 10, 64)
		if perr != nil || v < s {
			return 0, 0, false, ErrRangeNotSatisfiable
		}
		if v < e {
			e = v
		}
	}
	return s, e, true, nil
}
