package services

import "strings"

// PickMime picks the first candidate MIME that the renderer declared in
// ConnectionManager.Sink. Example: gmrender on BeagleBone knows only
// audio/x-wav, while on FiiO it also knows audio/wav. If Sink is empty or nothing
// matches, the first candidate.
func PickMime(sinks []string, candidates ...string) string {
	for _, c := range candidates {
		for _, s := range sinks {
			parts := strings.Split(s, ":")
			if len(parts) >= 3 && strings.EqualFold(strings.TrimSpace(parts[2]), c) {
				return c
			}
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}
