package player

import "testing"

// A wake reaches every stream; wakes that come while one is pending merge into it (the
// stream reads the state once anyway); a cancelled subscription is woken no more.
func TestNotifyWakesSubscribers(t *testing.T) {
	a, _, cancelA := Subscribe()
	b, _, cancelB := Subscribe()
	defer cancelB()
	Notify()
	Notify()
	for name, ch := range map[string]<-chan struct{}{"a": a, "b": b} {
		select {
		case <-ch:
		default:
			t.Fatalf("%s was not woken", name)
		}
		select {
		case <-ch:
			t.Fatalf("%s was woken twice for one read", name)
		default:
		}
	}
	cancelA()
	Notify()
	select {
	case <-a:
		t.Fatal("a cancelled subscription was woken")
	default:
	}
	select {
	case <-b:
	default:
		t.Fatal("b was not woken")
	}
}
