package platform

import "sync"

// Permission identifies a unified cross-platform device capability.
type Permission string

// Standard Gopkin cross-platform permissions.
const (
	PermLocation      Permission = "location"
	PermCamera        Permission = "camera"
	PermBluetooth     Permission = "bluetooth"
	PermNotifications Permission = "notifications"
	PermStorage       Permission = "storage"
)

// PermissionStatus represents the authorization state of a Permission.
type PermissionStatus string

const (
	Granted PermissionStatus = "granted"
	Denied  PermissionStatus = "denied"
	Prompt  PermissionStatus = "prompt"
)

// PermissionHandler abstracts OS-specific runtime permission checks and prompts.
type PermissionHandler interface {
	CheckPermission(perm Permission) PermissionStatus
	AskPermission(perm Permission) PermissionStatus
}

// DefaultPermissionHandler provides clean desktop default behavior:
// permissions start as Prompt and transition to Granted when requested.
type DefaultPermissionHandler struct {
	mu     sync.RWMutex
	states map[Permission]PermissionStatus
}

// NewDefaultPermissionHandler creates a new thread-safe DefaultPermissionHandler.
func NewDefaultPermissionHandler() *DefaultPermissionHandler {
	return &DefaultPermissionHandler{
		states: make(map[Permission]PermissionStatus),
	}
}

func (d *DefaultPermissionHandler) CheckPermission(perm Permission) PermissionStatus {
	d.mu.RLock()
	st, ok := d.states[perm]
	d.mu.RUnlock()
	if !ok {
		return Prompt
	}
	return st
}

func (d *DefaultPermissionHandler) AskPermission(perm Permission) PermissionStatus {
	d.mu.Lock()
	d.states[perm] = Granted
	d.mu.Unlock()
	return Granted
}

// MockPermissionHandler is a controllable PermissionHandler for unit tests.
type MockPermissionHandler struct {
	mu     sync.Mutex
	States map[Permission]PermissionStatus
	OnAsk  func(perm Permission) PermissionStatus
	Asked  []Permission
}

// NewMockPermissionHandler creates a MockPermissionHandler ready for testing.
func NewMockPermissionHandler() *MockPermissionHandler {
	return &MockPermissionHandler{
		States: make(map[Permission]PermissionStatus),
	}
}

func (m *MockPermissionHandler) CheckPermission(perm Permission) PermissionStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.States[perm]; ok {
		return st
	}
	return Prompt
}

func (m *MockPermissionHandler) AskPermission(perm Permission) PermissionStatus {
	m.mu.Lock()
	m.Asked = append(m.Asked, perm)
	onAsk := m.OnAsk
	m.mu.Unlock()

	if onAsk != nil {
		st := onAsk(perm)
		m.mu.Lock()
		m.States[perm] = st
		m.mu.Unlock()
		return st
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.States[perm]; ok && st != Prompt {
		return st
	}
	m.States[perm] = Granted
	return Granted
}

var (
	permMu             sync.RWMutex
	activePermHandler  PermissionHandler = NewDefaultPermissionHandler()
)

// SetPermissionHandler replaces the active platform PermissionHandler.
// Passing nil restores a fresh DefaultPermissionHandler.
func SetPermissionHandler(h PermissionHandler) {
	permMu.Lock()
	if h == nil {
		activePermHandler = NewDefaultPermissionHandler()
	} else {
		activePermHandler = h
	}
	permMu.Unlock()
}

// ResetPermissionHandler restores a fresh DefaultPermissionHandler.
func ResetPermissionHandler() {
	SetPermissionHandler(nil)
}

// CheckPermission queries the current authorization status of perm without prompting.
func CheckPermission(perm Permission) PermissionStatus {
	permMu.RLock()
	h := activePermHandler
	permMu.RUnlock()
	return h.CheckPermission(perm)
}

// AskPermission requests authorization for perm and returns the resulting status.
func AskPermission(perm Permission) PermissionStatus {
	permMu.RLock()
	h := activePermHandler
	permMu.RUnlock()
	return h.AskPermission(perm)
}
