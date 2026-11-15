package services

// SetStarred returns the starred folder list with path added (on) or removed (off).
// The input is never changed: config readers may still hold it. A path is kept once.
func SetStarred(list []string, path string, on bool) []string {
	out := make([]string, 0, len(list)+1)
	for _, p := range list {
		if p != path {
			out = append(out, p)
		}
	}
	if on {
		out = append(out, path)
	}
	return out
}

// StarredSet makes a lookup set of the starred folder list.
func StarredSet(list []string) map[string]bool {
	set := make(map[string]bool, len(list))
	for _, p := range list {
		set[p] = true
	}
	return set
}
