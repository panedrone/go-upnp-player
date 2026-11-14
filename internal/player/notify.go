package player

import "sync"

// The page's event streams (GET /api/events, ADR-026) are woken here: after every poll of
// the renderer and after every command, the state may have changed. A wake carries nothing;
// the stream takes a Snapshot and sends it only if it differs from the last one sent.

var (
	subsMu sync.Mutex
	subs   = map[chan struct{}]struct{}{}
	// streamsDone is closed at shutdown: open streams end, so they do not hold the
	// server's graceful stop for its whole timeout
	streamsDone = make(chan struct{})
	endOnce     sync.Once
)

// Subscribe returns a channel woken when the state may have changed, a channel closed at
// shutdown, and the func that ends the subscription.
func Subscribe() (wake <-chan struct{}, done <-chan struct{}, cancel func()) {
	ch := make(chan struct{}, 1) // one pending wake is enough: the stream reads the state then
	subsMu.Lock()
	subs[ch] = struct{}{}
	subsMu.Unlock()
	return ch, streamsDone, func() {
		subsMu.Lock()
		delete(subs, ch)
		subsMu.Unlock()
	}
}

// Notify wakes every stream; a stream already woken is not woken twice.
func Notify() {
	subsMu.Lock()
	defer subsMu.Unlock()
	for ch := range subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// EndStreams ends every open stream; called once, at process shutdown.
func EndStreams() {
	endOnce.Do(func() { close(streamsDone) })
}
