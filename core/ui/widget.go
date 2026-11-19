package ui

import "gopkin/core/platform"

// Rect represents a 2D bounding box in logical pixels.
type Rect struct {
	X, Y, W, H float32
}

// Contains reports whether the point (x, y) lies inside the rectangle.
func (r Rect) Contains(x, y float32) bool {
	return x >= r.X && x <= r.X+r.W && y >= r.Y && y <= r.Y+r.H
}

// Widget is the core interface implemented by all Gopkin UI elements and layout nodes.
type Widget interface {
	// Type returns the widget kind identifier (e.g. "text", "button", "row").
	Type() string
	// Layout computes and stores the widget's bounding box within bounds and returns the occupied Rect.
	Layout(bounds Rect) Rect
	// Render draws the widget onto the given platform.Driver.
	Render(driver platform.Driver)
	// HandleEvent processes a platform event and returns true if the event was consumed.
	HandleEvent(ev platform.Event) bool
}

// Theme defines Gopkin's strict monochrome color palette.
type Theme struct {
	Primary     platform.Color // #000000
	Background  platform.Color // #FFFFFF
	Accent      platform.Color // #B5B5B5
	Surface     platform.Color // #F5F5F5
	DarkSurface platform.Color // #111111
}

// DefaultTheme is the default Gopkin Black & White monochrome theme.
var DefaultTheme = Theme{
	Primary:     platform.ColorBlack,
	Background:  platform.ColorWhite,
	Accent:      platform.ColorAccentGrey,
	Surface:     platform.ColorSurfaceLight,
	DarkSurface: platform.ColorSurfaceDark,
}

