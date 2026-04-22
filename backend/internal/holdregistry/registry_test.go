package holdregistry

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestRegisterReceivesSignal(t *testing.T) {
	r := New()
	ch := r.Register("h1")
	defer r.Cancel("h1")
	go func() {
		time.Sleep(10 * time.Millisecond)
		r.Signal("h1", Decision{Allow: true})
	}()
	select {
	case d := <-ch:
		if !d.Allow {
			t.Fatalf("got %+v, want Allow=true", d)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func TestSignalUnknownIdNoPanic(t *testing.T) {
	r := New()
	r.Signal("does-not-exist", Decision{Allow: false}) // must not panic
}

func TestDoubleSignalNoPanic(t *testing.T) {
	r := New()
	ch := r.Register("h1")
	defer r.Cancel("h1")
	r.Signal("h1", Decision{Allow: true})
	r.Signal("h1", Decision{Allow: false}) // dropped (buffer full); no panic
	d := <-ch
	if !d.Allow {
		t.Fatalf("first signal should win, got %+v", d)
	}
}

func TestCancelRemovesEntry(t *testing.T) {
	r := New()
	r.Register("h1")
	r.Cancel("h1")
	r.Signal("h1", Decision{Allow: true}) // no-op now; receiver gone
}

func TestConcurrentRegisterSignal(t *testing.T) {
	r := New()
	const N = 100
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		id := "h" + strconv.Itoa(i)
		ch := r.Register(id)
		go func() {
			defer wg.Done()
			defer r.Cancel(id)
			d := <-ch
			if !d.Allow {
				t.Errorf("expected allow")
			}
		}()
		go r.Signal(id, Decision{Allow: true})
	}
	wg.Wait()
}

