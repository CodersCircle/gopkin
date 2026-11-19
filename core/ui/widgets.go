package ui

import (
	"fmt"
	"strconv"

	"gopkin/core/platform"
	"gopkin/core/state"
)

// TextWidget renders static or reactive text.
type TextWidget struct {
	source    any
	fontSize  float32
	color     platform.Color
	isHeading bool
	bounds    Rect
}

// Heading creates a monochrome heading widget (24px primary black).
func Heading(content any) *TextWidget {
	return &TextWidget{
		source:    content,
		fontSize:  24,
		color:     DefaultTheme.Primary,
		isHeading: true,
	}
}

// Text creates a monochrome text widget (16px primary black).
func Text(content any) *TextWidget {
	return &TextWidget{
		source:   content,
		fontSize: 16,
		color:    DefaultTheme.Primary,
	}
}

func (t *TextWidget) Type() string {
	if t.isHeading {
		return "heading"
	}
	return "text"
}

func (t *TextWidget) Bounds() Rect { return t.bounds }

// Muted styles the text with the monochrome accent grey (#B5B5B5).
func (t *TextWidget) Muted() *TextWidget {
	t.color = DefaultTheme.Accent
	return t
}

// Content resolves the current text value from static or reactive sources.
func (t *TextWidget) Content() string {
	return resolveText(t.source)
}

func (t *TextWidget) Layout(bounds Rect) Rect {
	h := t.fontSize * 1.5
	t.bounds = Rect{X: bounds.X, Y: bounds.Y, W: bounds.W, H: h}
	return t.bounds
}

func (t *TextWidget) Render(driver platform.Driver) {
	driver.DrawText(t.bounds.X, t.bounds.Y, t.Content(), t.fontSize, t.color)
}

func (t *TextWidget) HandleEvent(platform.Event) bool { return false }

func resolveText(src any) string {
	switch v := src.(type) {
	case nil:
		return ""
	case string:
		return v
	case *state.State[string]:
		if v == nil {
			return ""
		}
		return v.Get()
	case *state.NumState:
		if v == nil {
			return "0"
		}
		return strconv.FormatFloat(v.Val(), 'f', -1, 64)
	case func() string:
		if v == nil {
			return ""
		}
		return v()
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

// CardWidget is a monochrome surface container with padding and border.
type CardWidget struct {
	content *ColumnWidget
	dark    bool
	padding float32
	bounds  Rect
}

// Card creates a surface card containing the given child widgets.
func Card(children ...Widget) *CardWidget {
	col := Column(children...).Gap(defaultGap)
	return &CardWidget{
		content: col,
		padding: 16,
	}
}

func (c *CardWidget) Type() string       { return "card" }
func (c *CardWidget) Bounds() Rect       { return c.bounds }
func (c *CardWidget) Children() []Widget { return c.content.Children() }

// Dark switches the card surface to dark monochrome (#111111).
func (c *CardWidget) Dark() *CardWidget {
	c.dark = true
	return c
}

// Padding sets the card inner padding.
func (c *CardWidget) Padding(px float32) *CardWidget {
	c.padding = px
	return c
}

func (c *CardWidget) Layout(bounds Rect) Rect {
	inner := Rect{
		X: bounds.X + c.padding,
		Y: bounds.Y + c.padding,
		W: max(0, bounds.W-c.padding*2),
		H: max(0, bounds.H-c.padding*2),
	}
	used := c.content.Layout(inner)
	c.bounds = Rect{
		X: bounds.X,
		Y: bounds.Y,
		W: bounds.W,
		H: used.H + c.padding*2,
	}
	return c.bounds
}

func (c *CardWidget) Render(driver platform.Driver) {
	// Outer border (#B5B5B5)
	driver.DrawRect(c.bounds.X, c.bounds.Y, c.bounds.W, c.bounds.H, DefaultTheme.Accent)

	// Inner surface (#F5F5F5 light or #111111 dark)
	bg := DefaultTheme.Surface
	if c.dark {
		bg = DefaultTheme.DarkSurface
	}
	driver.DrawRect(c.bounds.X+1, c.bounds.Y+1, max(0, c.bounds.W-2), max(0, c.bounds.H-2), bg)

	c.content.Render(driver)
}

func (c *CardWidget) HandleEvent(ev platform.Event) bool {
	return c.content.HandleEvent(ev)
}

// ButtonWidget is a monochrome interactive button.
type ButtonWidget struct {
	label         string
	onClick       func()
	onDoubleClick func()
	fullWidth     bool
	bounds        Rect
}

// Btn creates a 1-liner button with a label and optional click callback.
func Btn(label string, onClick ...func()) *ButtonWidget {
	b := &ButtonWidget{label: label}
	if len(onClick) > 0 {
		b.onClick = onClick[0]
	}
	return b
}

func (b *ButtonWidget) Type() string      { return "button" }
func (b *ButtonWidget) Bounds() Rect      { return b.bounds }
func (b *ButtonWidget) Label() string     { return b.label }
func (b *ButtonWidget) IsFullWidth() bool { return b.fullWidth }

// OnClick sets or replaces the single-click callback.
func (b *ButtonWidget) OnClick(fn func()) *ButtonWidget {
	b.onClick = fn
	return b
}

// OnDoubleClick sets the double-click callback.
func (b *ButtonWidget) OnDoubleClick(fn func()) *ButtonWidget {
	b.onDoubleClick = fn
	return b
}

// FullWidth stretches the button across the full available container width.
func (b *ButtonWidget) FullWidth() *ButtonWidget {
	b.fullWidth = true
	return b
}

func (b *ButtonWidget) Layout(bounds Rect) Rect {
	w := bounds.W
	if !b.fullWidth {
		intrinsicW := max(float32(100), float32(len(b.label))*9+32)
		if intrinsicW < w {
			w = intrinsicW
		}
	}
	b.bounds = Rect{X: bounds.X, Y: bounds.Y, W: w, H: 40}
	return b.bounds
}

func (b *ButtonWidget) Render(driver platform.Driver) {
	// Primary black background (#000000) with pure white label (#FFFFFF)
	driver.DrawRect(b.bounds.X, b.bounds.Y, b.bounds.W, b.bounds.H, DefaultTheme.Primary)
	driver.DrawText(b.bounds.X+16, b.bounds.Y+12, b.label, 14, DefaultTheme.Background)
}

func (b *ButtonWidget) HandleEvent(ev platform.Event) bool {
	if !b.bounds.Contains(ev.X, ev.Y) {
		return false
	}
	switch ev.Type {
	case platform.EventClick:
		if b.onClick != nil {
			b.onClick()
		}
		return true
	case platform.EventDoubleClick:
		if b.onDoubleClick != nil {
			b.onDoubleClick()
		}
		return true
	default:
		return false
	}
}

// SliderWidget is a monochrome range slider bound to a NumState.
type SliderWidget struct {
	min      float64
	max      float64
	val      *state.NumState
	onChange func(float64)
	dragging bool
	bounds   Rect
}

// Slider creates a reactive slider in [min, max] bound to target.
// If target is nil, an internal NumState initialized to min is created.
func Slider(minVal, maxVal float64, target *state.NumState) *SliderWidget {
	if maxVal < minVal {
		minVal, maxVal = maxVal, minVal
	}
	if target == nil {
		target = state.NewNumState(minVal)
	}
	s := &SliderWidget{
		min: minVal,
		max: maxVal,
		val: target,
	}
	s.SetValue(target.Val())
	return s
}

func (s *SliderWidget) Type() string   { return "slider" }
func (s *SliderWidget) Bounds() Rect   { return s.bounds }
func (s *SliderWidget) Value() float64 { return s.val.Val() }

// OnChange registers a callback invoked when the slider value changes.
func (s *SliderWidget) OnChange(fn func(float64)) *SliderWidget {
	s.onChange = fn
	return s
}

// SetValue clamps v to [min, max], updates the bound NumState, and invokes OnChange.
func (s *SliderWidget) SetValue(v float64) {
	if v < s.min {
		v = s.min
	}
	if v > s.max {
		v = s.max
	}
	s.val.Set(v)
	if s.onChange != nil {
		s.onChange(v)
	}
}

func (s *SliderWidget) Layout(bounds Rect) Rect {
	s.bounds = Rect{X: bounds.X, Y: bounds.Y, W: bounds.W, H: 32}
	return s.bounds
}

func (s *SliderWidget) Render(driver platform.Driver) {
	trackY := s.bounds.Y + s.bounds.H/2 - 2
	// Track (#B5B5B5)
	driver.DrawRect(s.bounds.X, trackY, s.bounds.W, 4, DefaultTheme.Accent)

	ratio := float32(0)
	if s.max > s.min {
		ratio = float32((s.Value() - s.min) / (s.max - s.min))
	}
	fillW := s.bounds.W * ratio

	// Active fill (#000000)
	driver.DrawRect(s.bounds.X, trackY, fillW, 4, DefaultTheme.Primary)
	// Thumb (#000000)
	thumbX := s.bounds.X + fillW - 6
	thumbY := s.bounds.Y + s.bounds.H/2 - 6
	driver.DrawRect(thumbX, thumbY, 12, 12, DefaultTheme.Primary)
}

func (s *SliderWidget) HandleEvent(ev platform.Event) bool {
	switch ev.Type {
	case platform.EventMouseDown, platform.EventClick:
		if !s.bounds.Contains(ev.X, ev.Y) {
			return false
		}
		if ev.Type == platform.EventMouseDown {
			s.dragging = true
		}
		s.updateFromX(ev.X)
		return true
	case platform.EventMouseMove:
		if !s.dragging {
			return false
		}
		s.updateFromX(ev.X)
		return true
	case platform.EventMouseUp:
		if s.dragging {
			s.dragging = false
			return true
		}
		return false
	default:
		return false
	}
}

func (s *SliderWidget) updateFromX(x float32) {
	if s.bounds.W <= 0 {
		return
	}
	ratio := float64((x - s.bounds.X) / s.bounds.W)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	s.SetValue(s.min + ratio*(s.max-s.min))
}

// PopupWidget displays a centered monochrome modal dialog.
type PopupWidget struct {
	title   string
	content *ColumnWidget
	open    bool
	onClose func()
	bounds  Rect
}

// Popup creates a 1-liner modal dialog.
// Args may include Widgets, strings (converted to Text), func() (onClose handler), or other values.
func Popup(title string, args ...any) *PopupWidget {
	p := &PopupWidget{
		title: title,
		open:  true,
	}
	var children []Widget
	for _, arg := range args {
		switch v := arg.(type) {
		case nil:
			continue
		case Widget:
			children = append(children, v)
		case func():
			p.onClose = v
		default:
			children = append(children, Text(v))
		}
	}
	p.content = Column(children...).Gap(defaultGap)
	return p
}

func (p *PopupWidget) Type() string       { return "popup" }
func (p *PopupWidget) Title() string      { return p.title }
func (p *PopupWidget) Bounds() Rect       { return p.bounds }
func (p *PopupWidget) Children() []Widget { return p.content.Children() }
func (p *PopupWidget) IsOpen() bool       { return p.open }

// Show opens the popup.
func (p *PopupWidget) Show() *PopupWidget {
	p.open = true
	return p
}

// Close closes the popup and triggers OnClose if set.
func (p *PopupWidget) Close() *PopupWidget {
	if p.open {
		p.open = false
		if p.onClose != nil {
			p.onClose()
		}
	}
	return p
}

// OnClose sets a callback invoked when the popup closes.
func (p *PopupWidget) OnClose(fn func()) *PopupWidget {
	p.onClose = fn
	return p
}

func (p *PopupWidget) Layout(bounds Rect) Rect {
	if !p.open {
		p.bounds = Rect{}
		return p.bounds
	}

	const padding float32 = 16
	const headerH float32 = 36

	dialogW := min(float32(420), max(0, bounds.W-32))
	contentRect := p.content.Layout(Rect{
		X: 0,
		Y: 0,
		W: max(0, dialogW-padding*2),
		H: max(0, bounds.H-headerH-padding*3),
	})

	dialogH := headerH + padding*2 + contentRect.H
	x := bounds.X + max(0, (bounds.W-dialogW)/2)
	y := bounds.Y + max(0, (bounds.H-dialogH)/2)

	p.bounds = Rect{X: x, Y: y, W: dialogW, H: dialogH}
	p.content.Layout(Rect{
		X: x + padding,
		Y: y + headerH + padding,
		W: max(0, dialogW-padding*2),
		H: contentRect.H,
	})
	return p.bounds
}

func (p *PopupWidget) Render(driver platform.Driver) {
	if !p.open {
		return
	}
	// Outer monochrome border (#000000) and pure white surface (#FFFFFF)
	driver.DrawRect(p.bounds.X, p.bounds.Y, p.bounds.W, p.bounds.H, DefaultTheme.Primary)
	driver.DrawRect(p.bounds.X+2, p.bounds.Y+2, max(0, p.bounds.W-4), max(0, p.bounds.H-4), DefaultTheme.Background)

	// Title and accent divider (#B5B5B5)
	driver.DrawText(p.bounds.X+16, p.bounds.Y+10, p.title, 18, DefaultTheme.Primary)
	driver.DrawRect(p.bounds.X+2, p.bounds.Y+36, max(0, p.bounds.W-4), 1, DefaultTheme.Accent)

	p.content.Render(driver)
}

func (p *PopupWidget) HandleEvent(ev platform.Event) bool {
	if !p.open {
		return false
	}
	if p.content.HandleEvent(ev) {
		return true
	}
	// Consume pointer events inside the open modal dialog.
	if ev.Type == platform.EventClick || ev.Type == platform.EventDoubleClick || ev.Type == platform.EventMouseDown {
		return p.bounds.Contains(ev.X, ev.Y)
	}
	return false
}

