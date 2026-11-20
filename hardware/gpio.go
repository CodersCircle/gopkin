package hardware

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// PinDirection defines whether a GPIO pin is configured for input or output.
type PinDirection string

const (
	PinInput  PinDirection = "in"
	PinOutput PinDirection = "out"
)

// PinLevel represents a digital logic level on a GPIO pin.
type PinLevel int

const (
	Low  PinLevel = 0
	High PinLevel = 1
)

// GPIO is the unified Pure-Go hardware GPIO interface for Raspberry Pi, Linux SBCs,
// and desktop development/testing mocks.
type GPIO interface {
	PinMode(pin int, mode PinDirection) error
	DigitalWrite(pin int, level PinLevel) error
	DigitalRead(pin int) (PinLevel, error)
	Toggle(pin int) (PinLevel, error)
	Close() error
}

// OpenGPIO returns a LinuxSysfsGPIO on Linux when /sys/class/gpio is available,
// and cleanly falls back to MockGPIO on Windows, macOS, or headless environments.
func OpenGPIO() GPIO {
	if runtime.GOOS == "linux" {
		if info, err := os.Stat("/sys/class/gpio"); err == nil && info.IsDir() {
			return NewLinuxSysfsGPIO("/sys/class/gpio")
		}
	}
	return NewMockGPIO()
}

// --- Pure-Go Linux Sysfs GPIO (Zero CGO) ---

// LinuxSysfsGPIO controls Linux GPIO pins via /sys/class/gpio using pure Go file I/O.
type LinuxSysfsGPIO struct {
	mu       sync.Mutex
	basePath string
	exported map[int]bool
	closed   bool
}

// NewLinuxSysfsGPIO creates a LinuxSysfsGPIO rooted at basePath (usually "/sys/class/gpio").
func NewLinuxSysfsGPIO(basePath string) *LinuxSysfsGPIO {
	if strings.TrimSpace(basePath) == "" {
		basePath = "/sys/class/gpio"
	}
	return &LinuxSysfsGPIO{
		basePath: basePath,
		exported: make(map[int]bool),
	}
}

func (g *LinuxSysfsGPIO) ensureExportedLocked(pin int) error {
	if pin < 0 {
		return fmt.Errorf("hardware/gpio: invalid pin %d", pin)
	}
	if g.closed {
		return errors.New("hardware/gpio: controller is closed")
	}
	if g.exported[pin] {
		return nil
	}
	pinDir := filepath.Join(g.basePath, "gpio"+strconv.Itoa(pin))
	if _, err := os.Stat(pinDir); errors.Is(err, os.ErrNotExist) {
		exportFile := filepath.Join(g.basePath, "export")
		if err := os.WriteFile(exportFile, []byte(strconv.Itoa(pin)), 0o644); err != nil {
			return fmt.Errorf("hardware/gpio: export pin %d: %w", pin, err)
		}
	}
	g.exported[pin] = true
	return nil
}

// PinMode exports pin (if needed) and writes its direction ("in" or "out").
func (g *LinuxSysfsGPIO) PinMode(pin int, mode PinDirection) error {
	if mode != PinInput && mode != PinOutput {
		return fmt.Errorf("hardware/gpio: invalid pin mode %q", mode)
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	if err := g.ensureExportedLocked(pin); err != nil {
		return err
	}
	dirPath := filepath.Join(g.basePath, "gpio"+strconv.Itoa(pin), "direction")
	if err := os.WriteFile(dirPath, []byte(string(mode)), 0o644); err != nil {
		return fmt.Errorf("hardware/gpio: set direction on pin %d: %w", pin, err)
	}
	return nil
}

// DigitalWrite writes Low (0) or High (1) to pin.
func (g *LinuxSysfsGPIO) DigitalWrite(pin int, level PinLevel) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if err := g.ensureExportedLocked(pin); err != nil {
		return err
	}
	val := "0"
	if level != Low {
		val = "1"
	}
	valPath := filepath.Join(g.basePath, "gpio"+strconv.Itoa(pin), "value")
	if err := os.WriteFile(valPath, []byte(val), 0o644); err != nil {
		return fmt.Errorf("hardware/gpio: write pin %d: %w", pin, err)
	}
	return nil
}

// DigitalRead reads the current logic level (Low or High) from pin.
func (g *LinuxSysfsGPIO) DigitalRead(pin int) (PinLevel, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if err := g.ensureExportedLocked(pin); err != nil {
		return Low, err
	}
	return g.readLocked(pin)
}

func (g *LinuxSysfsGPIO) readLocked(pin int) (PinLevel, error) {
	valPath := filepath.Join(g.basePath, "gpio"+strconv.Itoa(pin), "value")
	raw, err := os.ReadFile(valPath)
	if err != nil {
		return Low, fmt.Errorf("hardware/gpio: read pin %d: %w", pin, err)
	}
	if strings.TrimSpace(string(raw)) == "1" {
		return High, nil
	}
	return Low, nil
}

// Toggle reads pin's current level, writes the inverted level, and returns the new level.
func (g *LinuxSysfsGPIO) Toggle(pin int) (PinLevel, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if err := g.ensureExportedLocked(pin); err != nil {
		return Low, err
	}
	cur, err := g.readLocked(pin)
	if err != nil {
		return Low, err
	}
	next := High
	if cur == High {
		next = Low
	}
	val := "0"
	if next == High {
		val = "1"
	}
	valPath := filepath.Join(g.basePath, "gpio"+strconv.Itoa(pin), "value")
	if err := os.WriteFile(valPath, []byte(val), 0o644); err != nil {
		return Low, fmt.Errorf("hardware/gpio: toggle pin %d: %w", pin, err)
	}
	return next, nil
}

// Close unexports all pins opened by this LinuxSysfsGPIO instance.
func (g *LinuxSysfsGPIO) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	g.closed = true
	unexportPath := filepath.Join(g.basePath, "unexport")
	var firstErr error
	for pin := range g.exported {
		if err := os.WriteFile(unexportPath, []byte(strconv.Itoa(pin)), 0o644); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	g.exported = make(map[int]bool)
	return firstErr
}

// --- MockGPIO for Desktop Dev & Unit Tests ---

// MockGPIO is a thread-safe in-memory GPIO controller for desktop development and testing.
type MockGPIO struct {
	mu     sync.RWMutex
	modes  map[int]PinDirection
	levels map[int]PinLevel
	closed bool
}

// NewMockGPIO creates a new MockGPIO instance.
func NewMockGPIO() *MockGPIO {
	return &MockGPIO{
		modes:  make(map[int]PinDirection),
		levels: make(map[int]PinLevel),
	}
}

func (m *MockGPIO) PinMode(pin int, mode PinDirection) error {
	if pin < 0 {
		return fmt.Errorf("hardware/gpio: invalid pin %d", pin)
	}
	if mode != PinInput && mode != PinOutput {
		return fmt.Errorf("hardware/gpio: invalid pin mode %q", mode)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("hardware/gpio: controller is closed")
	}
	m.modes[pin] = mode
	return nil
}

// Mode returns the configured direction for pin (empty string if unset).
func (m *MockGPIO) Mode(pin int) PinDirection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.modes[pin]
}

func (m *MockGPIO) DigitalWrite(pin int, level PinLevel) error {
	if pin < 0 {
		return fmt.Errorf("hardware/gpio: invalid pin %d", pin)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("hardware/gpio: controller is closed")
	}
	if level != Low {
		level = High
	}
	m.levels[pin] = level
	return nil
}

func (m *MockGPIO) DigitalRead(pin int) (PinLevel, error) {
	if pin < 0 {
		return Low, fmt.Errorf("hardware/gpio: invalid pin %d", pin)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return Low, errors.New("hardware/gpio: controller is closed")
	}
	return m.levels[pin], nil
}

func (m *MockGPIO) Toggle(pin int) (PinLevel, error) {
	if pin < 0 {
		return Low, fmt.Errorf("hardware/gpio: invalid pin %d", pin)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Low, errors.New("hardware/gpio: controller is closed")
	}
	next := High
	if m.levels[pin] == High {
		next = Low
	}
	m.levels[pin] = next
	return next, nil
}

func (m *MockGPIO) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}
