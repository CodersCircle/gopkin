package platform

import (
	"errors"
	"sync"
)

// DrawRectCall records a single DrawRect invocation on MockDriver.
type DrawRectCall struct {
	X, Y, W, H float32
	Color      Color
}

// DrawTextCall records a single DrawText invocation on MockDriver.
type DrawTextCall struct {
	X, Y  float32
	Text  string
	Size  float32
	Color Color
}

// MockDriver is a lightweight, thread-safe Driver implementation for automated testing.
type MockDriver struct {
	mu           sync.Mutex
	Initialized  bool
	WindowTitle  string
	WindowWidth  int
	WindowHeight int
	Rects        []DrawRectCall
	Texts        []DrawTextCall
	Events       []Event
	FrameCount   int
	// MaxFrames limits RunLoop iterations when > 0. If <= 0, defaults to 1 frame.
	MaxFrames int
}

// NewMockDriver creates a MockDriver ready for testing.
func NewMockDriver() *MockDriver {
	return &MockDriver{MaxFrames: 1}
}

// Init marks the mock driver as initialized.
func (m *MockDriver) Init() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Initialized = true
	return nil
}

// CreateWindow records window metadata.
func (m *MockDriver) CreateWindow(title string, width, height int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.Initialized {
		return errors.New("platform: driver not initialized")
	}
	if width <= 0 || height <= 0 {
		return errors.New("platform: invalid window dimensions")
	}
	m.WindowTitle = title
	m.WindowWidth = width
	m.WindowHeight = height
	return nil
}

// DrawRect records a rectangle draw call.
func (m *MockDriver) DrawRect(x, y, w, h float32, color Color) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Rects = append(m.Rects, DrawRectCall{X: x, Y: y, W: w, H: h, Color: color})
}

// DrawText records a text draw call.
func (m *MockDriver) DrawText(x, y float32, text string, size float32, color Color) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Texts = append(m.Texts, DrawTextCall{X: x, Y: y, Text: text, Size: size, Color: color})
}

// PushEvent queues an event to be returned by the next PollEvents call.
func (m *MockDriver) PushEvent(ev Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Events = append(m.Events, ev)
}

// PushClick queues a pointer click event at (x, y).
func (m *MockDriver) PushClick(x, y float32) {
	m.PushEvent(Event{Type: EventClick, X: x, Y: y})
}

// PushKeyDown queues a normalized key-down event (e.g. KeyBackspace, KeyEnter, KeyTab).
func (m *MockDriver) PushKeyDown(key string) {
	m.PushEvent(Event{Type: EventKeyDown, Key: key})
}

// PushChar queues a printable UTF-8 character input event.
func (m *MockDriver) PushChar(r rune) {
	m.PushEvent(Event{Type: EventChar, Rune: r})
}

// TypeText queues an EventChar for each rune in s.
func (m *MockDriver) TypeText(s string) {
	for _, r := range s {
		m.PushChar(r)
	}
}

// PollEvents returns and clears all queued events.
func (m *MockDriver) PollEvents() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Events) == 0 {
		return nil
	}
	out := make([]Event, len(m.Events))
	copy(out, m.Events)
	m.Events = nil
	return out
}

// ResetDrawCalls clears recorded rect and text draw calls.
func (m *MockDriver) ResetDrawCalls() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Rects = nil
	m.Texts = nil
}

// RunLoop invokes onFrame until it returns false or MaxFrames is reached.
func (m *MockDriver) RunLoop(onFrame func() bool) error {
	m.mu.Lock()
	if !m.Initialized {
		m.mu.Unlock()
		return errors.New("platform: driver not initialized")
	}
	maxFrames := m.MaxFrames
	if maxFrames <= 0 {
		maxFrames = 1
	}
	m.mu.Unlock()

	for range maxFrames {
		m.mu.Lock()
		m.FrameCount++
		m.mu.Unlock()

		if onFrame != nil && !onFrame() {
			break
		}
	}
	return nil
}

