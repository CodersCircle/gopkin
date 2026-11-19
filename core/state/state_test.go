package state

import (
	"sync"
	"testing"
)

func TestStateGetSetAndSetTo(t *testing.T) {
	s := NewState("hello")
	if got := s.Get(); got != "hello" {
		t.Fatalf("expected initial value 'hello', got %q", got)
	}

	s.Set("world")
	if got := s.Get(); got != "world" {
		t.Fatalf("expected updated value 'world', got %q", got)
	}

	reset := s.SetTo("reset")
	reset()
	if got := s.Get(); got != "reset" {
		t.Fatalf("expected SetTo value 'reset', got %q", got)
	}
}

func TestStateSubscribeAndUnsubscribe(t *testing.T) {
	s := NewState(0)

	var seen []int
	unsub := s.Subscribe(func(v int) {
		seen = append(seen, v)
	})

	s.Set(1)
	s.Set(2)
	unsub()
	s.Set(3)

	if len(seen) != 2 || seen[0] != 1 || seen[1] != 2 {
		t.Fatalf("expected subscriber to see [1 2], got %v", seen)
	}
}

func TestStateSubscribeReentrantReadDoesNotDeadlock(t *testing.T) {
	s := NewState(10)
	var readInside int
	s.Subscribe(func(v int) {
		readInside = s.Get()
	})
	s.Set(42)
	if readInside != 42 {
		t.Fatalf("expected reentrant Get() to return 42, got %d", readInside)
	}
}

func TestNumStateIncDecVal(t *testing.T) {
	n := NewNumState(5)
	if n.Val() != 5 {
		t.Fatalf("expected initial Val 5, got %v", n.Val())
	}

	var last float64
	n.Subscribe(func(v float64) { last = v })

	n.Inc()
	if n.Val() != 6 || last != 6 {
		t.Fatalf("expected 6 after Inc, got Val=%v last=%v", n.Val(), last)
	}

	n.Dec()
	n.Dec()
	if n.Val() != 4 || last != 4 {
		t.Fatalf("expected 4 after two Decs, got Val=%v last=%v", n.Val(), last)
	}
}

func TestStateConcurrency(t *testing.T) {
	n := NewNumState(0)

	var mu sync.Mutex
	notifications := 0
	n.Subscribe(func(float64) {
		mu.Lock()
		notifications++
		mu.Unlock()
	})

	const goroutines = 20
	const steps = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range steps {
				n.Inc()
			}
		}()
	}
	wg.Wait()

	want := float64(goroutines * steps)
	if n.Val() != want {
		t.Fatalf("expected %v, got %v", want, n.Val())
	}
	if notifications != goroutines*steps {
		t.Fatalf("expected %d notifications, got %d", goroutines*steps, notifications)
	}
}

