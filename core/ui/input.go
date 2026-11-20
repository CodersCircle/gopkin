package ui

import (
	"strings"
	"sync"

	"gopkin/core/platform"
	"gopkin/core/state"
)

// Focusable is implemented by interactive widgets that can hold keyboard focus.
type Focusable interface {
	Widget
	IsFocused() bool
	SetFocused(focused bool)
}

var (
	focusMu     sync.Mutex
	activeFocus Focusable
)

func claimFocus(target Focusable) {
	focusMu.Lock()
	prev := activeFocus
	if prev == target {
		focusMu.Unlock()
		return
	}
	activeFocus = target
	focusMu.Unlock()

	if prev != nil {
		prev.SetFocused(false)
	}
}

func releaseFocus(target Focusable) {
	focusMu.Lock()
	if activeFocus == target {
		activeFocus = nil
	}
	focusMu.Unlock()
}

// ClearFocus blurs whichever widget currently holds keyboard focus.
func ClearFocus() {
	focusMu.Lock()
	prev := activeFocus
	activeFocus = nil
	focusMu.Unlock()

	if prev != nil {
		prev.SetFocused(false)
	}
}

// CycleFocus moves keyboard focus to the next (or previous, if reverse is true)
// Focusable descendant inside root in visual tree order.
func CycleFocus(root Widget, reverse bool) bool {
	focusables := collectFocusables(root)
	if len(focusables) == 0 {
		return false
	}

	curIdx := -1
	for i, f := range focusables {
		if f.IsFocused() {
			curIdx = i
			break
		}
	}

	var nextIdx int
	if curIdx < 0 {
		if reverse {
			nextIdx = len(focusables) - 1
		} else {
			nextIdx = 0
		}
	} else if reverse {
		nextIdx = (curIdx - 1 + len(focusables)) % len(focusables)
	} else {
		nextIdx = (curIdx + 1) % len(focusables)
	}

	focusables[nextIdx].SetFocused(true)
	return true
}

func collectFocusables(w Widget) []Focusable {
	if w == nil {
		return nil
	}
	if f, ok := w.(Focusable); ok {
		return []Focusable{f}
	}

	var out []Focusable
	switch node := w.(type) {
	case interface{ Children() []Widget }:
		for _, ch := range node.Children() {
			out = append(out, collectFocusables(ch)...)
		}
	case interface{ Child() Widget }:
		out = append(out, collectFocusables(node.Child())...)
	case interface{ Focusables() []Focusable }:
		out = append(out, node.Focusables()...)
	}
	return out
}

// TextInputWidget is an interactive monochrome text field with focus, caret,
// masking, reactive signal binding, and inline validation error support.
type TextInputWidget struct {
	placeholder string
	value       string
	bound       *state.Signal[string]
	onChange    func(string)
	onSubmit    func(string)
	password    bool
	errMsg      string
	focused     bool
	caret       int
	bounds      Rect
	boxBounds   Rect
}

// TextInput creates an interactive text input widget with an optional placeholder.
func TextInput(placeholder ...string) *TextInputWidget {
	ti := &TextInputWidget{}
	if len(placeholder) > 0 {
		ti.placeholder = placeholder[0]
	}
	return ti
}

func (t *TextInputWidget) Type() string          { return "text_input" }
func (t *TextInputWidget) Bounds() Rect          { return t.bounds }
func (t *TextInputWidget) IsFocused() bool       { return t.focused }
func (t *TextInputWidget) CaretPos() int         { return t.caret }
func (t *TextInputWidget) ErrorMessage() string  { return t.errMsg }
func (t *TextInputWidget) IsPassword() bool      { return t.password }
func (t *TextInputWidget) PlaceholderText() string { return t.placeholder }

// Placeholder sets the muted hint text shown when the field is empty.
func (t *TextInputWidget) Placeholder(text string) *TextInputWidget {
	t.placeholder = text
	return t
}

// Value returns the current text value (reading from the bound signal if present).
func (t *TextInputWidget) Value() string {
	if t.bound != nil {
		return t.bound.Get()
	}
	return t.value
}

// SetValue sets the input text, moves the caret to the end, updates any bound
// signal, and invokes OnChange if the value changed.
func (t *TextInputWidget) SetValue(val string) *TextInputWidget {
	prev := t.Value()
	t.value = val
	if t.bound != nil {
		t.bound.Set(val)
	}
	t.caret = len([]rune(val))
	if prev != val && t.onChange != nil {
		t.onChange(val)
	}
	return t
}

// Bind connects this input to a reactive *state.Signal[string].
func (t *TextInputWidget) Bind(sig *state.Signal[string]) *TextInputWidget {
	t.bound = sig
	if sig != nil {
		t.value = sig.Get()
		t.caret = len([]rune(t.value))
	}
	return t
}

// OnChange registers a callback invoked whenever the text value changes.
func (t *TextInputWidget) OnChange(fn func(string)) *TextInputWidget {
	t.onChange = fn
	return t
}

// OnSubmit registers a callback invoked when Enter is pressed while focused.
func (t *TextInputWidget) OnSubmit(fn func(string)) *TextInputWidget {
	t.onSubmit = fn
	return t
}

// Password toggles password masking (rendering '•' per character).
func (t *TextInputWidget) Password(enabled bool) *TextInputWidget {
	t.password = enabled
	return t
}

// Error sets or clears the inline validation error message displayed below the input.
func (t *TextInputWidget) Error(msg string) *TextInputWidget {
	t.errMsg = msg
	return t
}

// SetFocused sets keyboard focus on this input, blurring any previously focused input.
func (t *TextInputWidget) SetFocused(focused bool) {
	if focused {
		t.focused = true
		t.caret = min(t.caret, len([]rune(t.Value())))
		claimFocus(t)
	} else {
		t.focused = false
		releaseFocus(t)
	}
}

// Focus gives keyboard focus to this input and returns it for chaining.
func (t *TextInputWidget) Focus() *TextInputWidget {
	t.SetFocused(true)
	return t
}

// Blur removes keyboard focus from this input and returns it for chaining.
func (t *TextInputWidget) Blur() *TextInputWidget {
	t.SetFocused(false)
	return t
}

// DisplayText returns the rendered text (masked with '•' if Password mode is enabled).
func (t *TextInputWidget) DisplayText() string {
	val := t.Value()
	if val == "" {
		return ""
	}
	if t.password {
		return strings.Repeat("•", len([]rune(val)))
	}
	return val
}

func (t *TextInputWidget) Layout(bounds Rect) Rect {
	const boxH float32 = 40
	const errH float32 = 20

	t.boxBounds = Rect{X: bounds.X, Y: bounds.Y, W: bounds.W, H: boxH}
	totalH := boxH
	if t.errMsg != "" {
		totalH += errH
	}
	t.bounds = Rect{X: bounds.X, Y: bounds.Y, W: bounds.W, H: totalH}
	return t.bounds
}

func (t *TextInputWidget) Render(driver platform.Driver) {
	borderColor := DefaultTheme.Accent
	borderWidth := float32(1)
	if t.focused {
		borderColor = DefaultTheme.Primary
		borderWidth = 2
	}

	// Outer border / focus ring
	driver.DrawRect(t.boxBounds.X, t.boxBounds.Y, t.boxBounds.W, t.boxBounds.H, borderColor)
	// Inner background (#FFFFFF)
	driver.DrawRect(
		t.boxBounds.X+borderWidth,
		t.boxBounds.Y+borderWidth,
		max(0, t.boxBounds.W-borderWidth*2),
		max(0, t.boxBounds.H-borderWidth*2),
		DefaultTheme.Background,
	)

	textX := t.boxBounds.X + 12
	textY := t.boxBounds.Y + 12

	display := t.DisplayText()
	if display == "" {
		if t.placeholder != "" {
			driver.DrawText(textX, textY, t.placeholder, 14, DefaultTheme.Accent)
		}
	} else {
		driver.DrawText(textX, textY, display, 14, DefaultTheme.Primary)
	}

	// Caret rendering when focused
	if t.focused {
		caretOffset := float32(t.caret) * 8
		caretX := min(textX+caretOffset, t.boxBounds.X+max(0, t.boxBounds.W-10))
		driver.DrawRect(caretX, t.boxBounds.Y+8, 2, t.boxBounds.H-16, DefaultTheme.Primary)
	}

	// Inline validation error message below the input box
	if t.errMsg != "" {
		driver.DrawText(t.bounds.X+2, t.boxBounds.Y+t.boxBounds.H+4, t.errMsg, 12, ColorError)
	}
}

func (t *TextInputWidget) HandleEvent(ev platform.Event) bool {
	switch ev.Type {
	case platform.EventMouseDown, platform.EventClick:
		if t.boxBounds.Contains(ev.X, ev.Y) {
			t.SetFocused(true)
			// Position caret near clicked character offset.
			runes := []rune(t.Value())
			approxIdx := int((ev.X - (t.boxBounds.X + 12)) / 8)
			if approxIdx < 0 {
				approxIdx = 0
			}
			if approxIdx > len(runes) {
				approxIdx = len(runes)
			}
			t.caret = approxIdx
			return true
		}
		if t.focused {
			t.SetFocused(false)
		}
		return false

	case platform.EventChar:
		if !t.focused {
			return false
		}
		if ev.Rune < 0x20 || ev.Rune == 0x7F {
			return false
		}
		runes := []rune(t.Value())
		pos := clampInt(t.caret, 0, len(runes))
		updated := make([]rune, 0, len(runes)+1)
		updated = append(updated, runes[:pos]...)
		updated = append(updated, ev.Rune)
		updated = append(updated, runes[pos:]...)
		t.applyText(string(updated), pos+1)
		return true

	case platform.EventKeyDown:
		if !t.focused {
			return false
		}
		runes := []rune(t.Value())
		pos := clampInt(t.caret, 0, len(runes))
		switch ev.Key {
		case platform.KeyBackspace:
			if pos > 0 {
				updated := append(runes[:pos-1:pos-1], runes[pos:]...)
				t.applyText(string(updated), pos-1)
			}
			return true
		case platform.KeyDelete:
			if pos < len(runes) {
				updated := append(runes[:pos:pos], runes[pos+1:]...)
				t.applyText(string(updated), pos)
			}
			return true
		case platform.KeyLeft:
			if pos > 0 {
				t.caret = pos - 1
			}
			return true
		case platform.KeyRight:
			if pos < len(runes) {
				t.caret = pos + 1
			}
			return true
		case platform.KeyHome:
			t.caret = 0
			return true
		case platform.KeyEnd:
			t.caret = len(runes)
			return true
		case platform.KeyEnter:
			if t.onSubmit != nil {
				t.onSubmit(t.Value())
			}
			return true
		case platform.KeyEscape:
			t.SetFocused(false)
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func (t *TextInputWidget) applyText(val string, nextCaret int) {
	t.value = val
	if t.bound != nil {
		t.bound.Set(val)
	}
	t.caret = nextCaret
	if t.onChange != nil {
		t.onChange(val)
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
