//go:build !windows

package platform

// NewDefaultDriver returns the default platform Driver for non-Windows hosts.
func NewDefaultDriver() Driver {
	return NewMockDriver()
}
