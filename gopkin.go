package gopkin

import (
	"sync"

	"gopkin/core/platform"
	"gopkin/core/state"
	"gopkin/core/ui"
)

// Core type aliases for ergonomic dot-import usage (`import . "gopkin"`).
type (
	Widget            = ui.Widget
	Driver            = platform.Driver
	Focusable         = ui.Focusable
	FormField         = ui.FormField
	TableColumn       = ui.TableColumn
	RowAction         = ui.RowAction
	Permission        = platform.Permission
	PermissionStatus  = platform.PermissionStatus
	PermissionHandler = platform.PermissionHandler
)

// Unified cross-platform permission constants & statuses.
const (
	PermLocation      = platform.PermLocation
	PermCamera        = platform.PermCamera
	PermBluetooth     = platform.PermBluetooth
	PermNotifications = platform.PermNotifications
	PermStorage       = platform.PermStorage

	Granted = platform.Granted
	Denied  = platform.Denied
	Prompt  = platform.Prompt
)

// Unified permission helpers.
var (
	AskPermission        = platform.AskPermission
	CheckPermission      = platform.CheckPermission
	SetPermissionHandler = platform.SetPermissionHandler
)

// Reactive state constructors.

// State creates a new thread-safe generic reactive state container.
func State[T any](initial T) *state.State[T] {
	return state.NewState(initial)
}

// Signal creates a new thread-safe generic reactive signal container.
func Signal[T any](initial T) *state.Signal[T] {
	return state.NewSignal(initial)
}

// Num creates a new thread-safe reactive float64 state container.
func Num(initial float64) *state.NumState {
	return state.NewNumState(initial)
}

// Layout node re-exports.
var (
	Row    = ui.Row
	Column = ui.Column
	Center = ui.Center
	Grid   = ui.Grid
)

// 1-liner widget & Filament-style plug-and-play kit re-exports.
var (
	Heading       = ui.Heading
	Text          = ui.Text
	Card          = ui.Card
	Slider        = ui.Slider
	Btn           = ui.Btn
	Popup         = ui.Popup
	TextInput     = ui.TextInput
	Form          = ui.Form
	TextField     = ui.TextField
	PasswordField = ui.PasswordField
	SelectField   = ui.SelectField
	ToggleField   = ui.ToggleField
	DataTable     = ui.DataTable
	Col           = ui.Col
	TableAction   = ui.TableAction
	Badge         = ui.Badge
	Toast         = ui.Toast
	AuthCard      = ui.AuthCard
)

var (
	driverMu      sync.RWMutex
	defaultDriver platform.Driver
)

// SetDriver sets the package-level default platform.Driver used by Run.
func SetDriver(d platform.Driver) {
	driverMu.Lock()
	defaultDriver = d
	driverMu.Unlock()
}

func getDriver() platform.Driver {
	driverMu.RLock()
	d := defaultDriver
	driverMu.RUnlock()
	if d != nil {
		return d
	}
	return platform.NewMockDriver()
}

// Run initializes the default platform driver, opens a window, and runs the UI loop.
func Run(title string, widgets ...Widget) error {
	return RunWithDriver(getDriver(), title, widgets...)
}

// RunWithDriver executes a Gopkin UI tree using an explicit platform.Driver.
func RunWithDriver(driver platform.Driver, title string, widgets ...Widget) error {
	if driver == nil {
		driver = platform.NewMockDriver()
	}

	var root Widget
	switch len(widgets) {
	case 0:
		root = Column()
	case 1:
		root = widgets[0]
	default:
		root = Column(widgets...)
	}

	width, height := 800, 600
	if err := driver.Init(); err != nil {
		return err
	}
	if err := driver.CreateWindow(title, width, height); err != nil {
		return err
	}

	return driver.RunLoop(func() bool {
		// Compute layout before event hit-testing so widget bounds are current.
		root.Layout(ui.Rect{X: 0, Y: 0, W: float32(width), H: float32(height)})

		for _, ev := range driver.PollEvents() {
			switch ev.Type {
			case platform.EventQuit:
				return false
			case platform.EventResize:
				if ev.Width > 0 && ev.Height > 0 {
					width, height = ev.Width, ev.Height
					root.Layout(ui.Rect{X: 0, Y: 0, W: float32(width), H: float32(height)})
				}
			default:
				root.HandleEvent(ev)
			}
		}

		// Clear surface with monochrome pure white (#FFFFFF) and render root.
		driver.DrawRect(0, 0, float32(width), float32(height), platform.ColorWhite)
		root.Render(driver)
		return true
	})
}

