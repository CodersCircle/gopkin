//go:build windows

package platform

// NewDefaultDriver returns the native Pure-Go Win32 GUI Driver on Windows.
func NewDefaultDriver() Driver {
	return NewWin32Driver()
}
