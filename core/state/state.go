package state

import "sync"

type subscriber[T any] struct {
	id uint64
	fn func(T)
}

// State is a thread-safe generic reactive state container.
type State[T any] struct {
	mu     sync.RWMutex
	val    T
	nextID uint64
	subs   []subscriber[T]
}

// Signal is an alias for State for reactive signal-style usage.
type Signal[T any] = State[T]

// NewState creates a new reactive State initialized with initial.
func NewState[T any](initial T) *State[T] {
	return &State[T]{val: initial}
}

// NewSignal creates a new reactive Signal initialized with initial.
func NewSignal[T any](initial T) *Signal[T] {
	return NewState(initial)
}

// Get returns the current value in a thread-safe manner.
func (s *State[T]) Get() T {
	s.mu.RLock()
	v := s.val
	s.mu.RUnlock()
	return v
}

// Set updates the state to val and notifies all subscribers.
func (s *State[T]) Set(val T) {
	s.mu.Lock()
	s.val = val
	subs := make([]func(T), len(s.subs))
	for i, sub := range s.subs {
		subs[i] = sub.fn
	}
	s.mu.Unlock()

	for _, fn := range subs {
		fn(val)
	}
}

// SetTo returns a zero-argument callback that sets the state to val when invoked.
func (s *State[T]) SetTo(val T) func() {
	return func() {
		s.Set(val)
	}
}

// Update atomically transforms the current value using fn and notifies subscribers.
func (s *State[T]) Update(fn func(T) T) {
	if fn == nil {
		return
	}
	s.mu.Lock()
	s.val = fn(s.val)
	val := s.val
	subs := make([]func(T), len(s.subs))
	for i, sub := range s.subs {
		subs[i] = sub.fn
	}
	s.mu.Unlock()

	for _, subFn := range subs {
		subFn(val)
	}
}

// Subscribe registers a listener that is called whenever the state changes.
// It returns an unsubscribe function that removes the listener.
func (s *State[T]) Subscribe(fn func(T)) func() {
	if fn == nil {
		return func() {}
	}
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	s.subs = append(s.subs, subscriber[T]{id: id, fn: fn})
	s.mu.Unlock()

	return func() {
		s.mu.Lock()
		for i, sub := range s.subs {
			if sub.id == id {
				s.subs = append(s.subs[:i], s.subs[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
	}
}

// NumState is a reactive float64 state with numeric helpers.
type NumState struct {
	*State[float64]
}

// NewNumState creates a new NumState initialized with initial.
func NewNumState(initial float64) *NumState {
	return &NumState{State: NewState(initial)}
}

// Inc atomically increments the numeric value by 1 and notifies subscribers.
func (n *NumState) Inc() {
	n.Update(func(v float64) float64 { return v + 1 })
}

// Dec atomically decrements the numeric value by 1 and notifies subscribers.
func (n *NumState) Dec() {
	n.Update(func(v float64) float64 { return v - 1 })
}

// Val returns the current float64 value.
func (n *NumState) Val() float64 {
	return n.Get()
}

