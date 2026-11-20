package platform

// Color represents an RGBA color.
type Color struct {
	R, G, B, A uint8
}

// Gopkin Monochrome Design System palette constants.
var (
	ColorBlack        = Color{R: 0x00, G: 0x00, B: 0x00, A: 0xFF} // #000000
	ColorWhite        = Color{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF} // #FFFFFF
	ColorAccentGrey   = Color{R: 0xB5, G: 0xB5, B: 0xB5, A: 0xFF} // #B5B5B5
	ColorSurfaceLight = Color{R: 0xF5, G: 0xF5, B: 0xF5, A: 0xFF} // #F5F5F5
	ColorSurfaceDark  = Color{R: 0x11, G: 0x11, B: 0x11, A: 0xFF} // #111111
)

// EventType identifies the kind of platform input or window event.
type EventType int

const (
	EventNone EventType = iota
	EventQuit
	EventResize
	EventMouseDown
	EventMouseUp
	EventMouseMove
	EventClick
	EventDoubleClick
	EventKeyDown
	EventKeyUp
	EventChar
)

// Normalized key names reported on EventKeyDown / EventKeyUp.
const (
	KeyBackspace = "Backspace"
	KeyEnter     = "Enter"
	KeyTab       = "Tab"
	KeyEscape    = "Escape"
	KeyLeft      = "Left"
	KeyRight     = "Right"
	KeyUp        = "Up"
	KeyDown      = "Down"
	KeyDelete    = "Delete"
	KeyHome      = "Home"
	KeyEnd       = "End"
)

// Event represents a normalized platform event.
type Event struct {
	Type   EventType
	X, Y   float32
	Width  int
	Height int
	Key    string
	Rune   rune
	Shift  bool
}

// Driver defines the strict platform abstraction boundary for OS windowing,
// 2D drawing primitives, and event loop execution.
type Driver interface {
	Init() error
	CreateWindow(title string, width, height int) error
	DrawRect(x, y, w, h float32, color Color)
	DrawText(x, y float32, text string, size float32, color Color)
	PollEvents() []Event
	RunLoop(onFrame func() bool) error
}

