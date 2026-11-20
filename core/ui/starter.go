package ui

import (
	"fmt"
	"strconv"

	"gopkin/core/platform"
)

// StarterScreenWidget renders the default Gopkin interactive starter screen
// featuring the GOPKIN top bar with isometric cube logo, dark/light theme
// toggle, welcome hero, interactive counter (- / + / Refresh), and
// Documentation / Settings cards.
type StarterScreenWidget struct {
	appName   string
	count     int
	dark      bool
	statusMsg string

	bounds      Rect
	themeBtn    Rect
	decBtn      Rect
	incBtn      Rect
	resetBtn    Rect
	docsCard    Rect
	settingsCrd Rect
}

// StarterScreen creates the interactive Gopkin starter UI for appName.
func StarterScreen(appName string) *StarterScreenWidget {
	if appName == "" {
		appName = "Gopkin App"
	}
	return &StarterScreenWidget{
		appName:   appName,
		statusMsg: "Ready — click +, -, Refresh, or toggle Dark/Light mode.",
	}
}

func (s *StarterScreenWidget) Type() string  { return "starter_screen" }
func (s *StarterScreenWidget) Bounds() Rect  { return s.bounds }
func (s *StarterScreenWidget) Count() int    { return s.count }
func (s *StarterScreenWidget) IsDark() bool  { return s.dark }
func (s *StarterScreenWidget) Status() string { return s.statusMsg }

func (s *StarterScreenWidget) Layout(bounds Rect) Rect {
	s.bounds = bounds

	// Top bar right-aligned theme toggle button.
	const topBarH float32 = 56
	const toggleW float32 = 112
	s.themeBtn = Rect{
		X: bounds.X + max(16, bounds.W-toggleW-20),
		Y: bounds.Y + 12,
		W: toggleW,
		H: 32,
	}

	// Centered content column (max 560px wide).
	contentW := min(float32(560), max(280, bounds.W-48))
	contentX := bounds.X + max(24, (bounds.W-contentW)/2)

	// Counter card starts below top bar + hero section.
	counterCardY := bounds.Y + topBarH + 92
	btnY := counterCardY + 104
	const btnH float32 = 40
	const gap float32 = 12
	btnW := max(72, (contentW-48-gap*2)/3)
	btnStartX := contentX + 24

	s.decBtn = Rect{X: btnStartX, Y: btnY, W: btnW, H: btnH}
	s.incBtn = Rect{X: btnStartX + btnW + gap, Y: btnY, W: btnW, H: btnH}
	s.resetBtn = Rect{X: btnStartX + (btnW+gap)*2, Y: btnY, W: btnW, H: btnH}

	// Bottom Documentation & Settings cards side-by-side.
	cardsY := counterCardY + 176
	halfW := max(120, (contentW-16)/2)
	s.docsCard = Rect{X: contentX, Y: cardsY, W: halfW, H: 84}
	s.settingsCrd = Rect{X: contentX + halfW + 16, Y: cardsY, W: halfW, H: 84}

	return s.bounds
}

func (s *StarterScreenWidget) Render(driver platform.Driver) {
	// Adaptive Light / Dark palette with Gopkin #1B4EF5 brand blue accent.
	accentBlue := platform.Color{R: 0x1B, G: 0x4E, B: 0xF5, A: 0xFF}
	accentLight := platform.Color{R: 0x3B, G: 0x6B, B: 0xFF, A: 0xFF}
	bg := platform.ColorWhite
	surface := platform.ColorSurfaceLight
	border := platform.Color{R: 0xE2, G: 0xE5, B: 0xEB, A: 0xFF}
	textPrimary := platform.ColorBlack
	textMuted := platform.Color{R: 0x5A, G: 0x5F, B: 0x6B, A: 0xFF}
	btnTextLight := platform.ColorWhite

	if s.dark {
		bg = platform.Color{R: 0x09, G: 0x0B, B: 0x10, A: 0xFF}
		surface = platform.Color{R: 0x16, G: 0x1B, B: 0x26, A: 0xFF}
		border = platform.Color{R: 0x25, G: 0x2C, B: 0x3B, A: 0xFF}
		textPrimary = platform.Color{R: 0xF8, G: 0xFA, B: 0xFC, A: 0xFF}
		textMuted = platform.Color{R: 0x9A, G: 0xA3, B: 0xB5, A: 0xFF}
		accentBlue = accentLight
	}

	// Full-viewport background.
	driver.DrawRect(s.bounds.X, s.bounds.Y, s.bounds.W, s.bounds.H, bg)

	// Top bar.
	const topBarH float32 = 56
	driver.DrawRect(s.bounds.X, s.bounds.Y, s.bounds.W, topBarH, surface)
	driver.DrawRect(s.bounds.X, s.bounds.Y+topBarH-1, s.bounds.W, 1, border)

	// Isometric GOPKIN cube mark (built from clean geometric rects for 100% cross-driver fidelity).
	logoX := s.bounds.X + 20
	logoY := s.bounds.Y + 14
	driver.DrawRect(logoX+4, logoY, 20, 10, accentBlue)
	driver.DrawRect(logoX, logoY+10, 14, 16, platform.Color{R: 0x0F, G: 0x17, B: 0x2A, A: 0xFF})
	driver.DrawRect(logoX+14, logoY+10, 14, 16, accentLight)
	driver.DrawRect(logoX+11, logoY+10, 6, 6, platform.ColorWhite)

	driver.DrawText(logoX+38, s.bounds.Y+18, "GOPKIN", 18, textPrimary)

	// Theme toggle button.
	driver.DrawRect(s.themeBtn.X, s.themeBtn.Y, s.themeBtn.W, s.themeBtn.H, border)
	driver.DrawRect(s.themeBtn.X+1, s.themeBtn.Y+1, s.themeBtn.W-2, s.themeBtn.H-2, bg)
	toggleLabel := "Dark Mode"
	if s.dark {
		toggleLabel = "Light Mode"
	}
	driver.DrawText(s.themeBtn.X+16, s.themeBtn.Y+9, toggleLabel, 13, textPrimary)

	// Centered content column.
	contentW := min(float32(560), max(280, s.bounds.W-48))
	contentX := s.bounds.X + max(24, (s.bounds.W-contentW)/2)

	// Hero section.
	heroY := s.bounds.Y + topBarH + 22
	driver.DrawText(contentX, heroY, "Welcome to "+s.appName, 26, textPrimary)
	driver.DrawText(contentX, heroY+36, "Pure-Go Cross-Platform App — Zero CGO, Zero Dependencies", 14, textMuted)

	// Interactive Counter card.
	counterCardY := s.bounds.Y + topBarH + 92
	const counterCardH float32 = 160
	driver.DrawRect(contentX, counterCardY, contentW, counterCardH, border)
	driver.DrawRect(contentX+1, counterCardY+1, contentW-2, counterCardH-2, surface)

	driver.DrawText(contentX+24, counterCardY+18, "Interactive Counter", 15, textMuted)
	driver.DrawText(contentX+24, counterCardY+48, strconv.Itoa(s.count), 36, accentBlue)

	// Counter action buttons: "-", "+", "Refresh".
	drawStarterBtn(driver, s.decBtn, "-", border, bg, textPrimary)
	drawStarterBtn(driver, s.incBtn, "+", accentBlue, accentBlue, btnTextLight)
	drawStarterBtn(driver, s.resetBtn, "Refresh", border, bg, textPrimary)

	// Documentation card.
	driver.DrawRect(s.docsCard.X, s.docsCard.Y, s.docsCard.W, s.docsCard.H, border)
	driver.DrawRect(s.docsCard.X+1, s.docsCard.Y+1, s.docsCard.W-2, s.docsCard.H-2, surface)
	driver.DrawText(s.docsCard.X+16, s.docsCard.Y+18, "Documentation", 16, textPrimary)
	driver.DrawText(s.docsCard.X+16, s.docsCard.Y+44, "Guides, .gpk syntax & CLI", 13, textMuted)

	// Settings card.
	driver.DrawRect(s.settingsCrd.X, s.settingsCrd.Y, s.settingsCrd.W, s.settingsCrd.H, border)
	driver.DrawRect(s.settingsCrd.X+1, s.settingsCrd.Y+1, s.settingsCrd.W-2, s.settingsCrd.H-2, surface)
	driver.DrawText(s.settingsCrd.X+16, s.settingsCrd.Y+18, "Settings", 16, textPrimary)
	themeDesc := "Theme: Light (click to switch)"
	if s.dark {
		themeDesc = "Theme: Dark (click to switch)"
	}
	driver.DrawText(s.settingsCrd.X+16, s.settingsCrd.Y+44, themeDesc, 13, textMuted)

	// Live status line below cards.
	driver.DrawText(contentX, s.docsCard.Y+s.docsCard.H+18, s.statusMsg, 13, textMuted)
}

func drawStarterBtn(driver platform.Driver, r Rect, label string, border, bg, fg platform.Color) {
	driver.DrawRect(r.X, r.Y, r.W, r.H, border)
	driver.DrawRect(r.X+1, r.Y+1, max(0, r.W-2), max(0, r.H-2), bg)
	textOffset := max(float32(10), (r.W-float32(len(label))*8)/2)
	driver.DrawText(r.X+textOffset, r.Y+12, label, 15, fg)
}

func (s *StarterScreenWidget) HandleEvent(ev platform.Event) bool {
	if ev.Type == platform.EventKeyDown {
		switch ev.Key {
		case platform.KeyUp:
			s.count++
			s.statusMsg = fmt.Sprintf("Counter incremented to %d.", s.count)
			return true
		case platform.KeyDown:
			s.count--
			s.statusMsg = fmt.Sprintf("Counter decremented to %d.", s.count)
			return true
		}
		return false
	}

	if ev.Type != platform.EventClick {
		return false
	}

	switch {
	case s.themeBtn.Contains(ev.X, ev.Y):
		s.dark = !s.dark
		if s.dark {
			s.statusMsg = "Switched to Dark Mode."
		} else {
			s.statusMsg = "Switched to Light Mode."
		}
		return true
	case s.incBtn.Contains(ev.X, ev.Y):
		s.count++
		s.statusMsg = fmt.Sprintf("Counter incremented to %d.", s.count)
		return true
	case s.decBtn.Contains(ev.X, ev.Y):
		s.count--
		s.statusMsg = fmt.Sprintf("Counter decremented to %d.", s.count)
		return true
	case s.resetBtn.Contains(ev.X, ev.Y):
		s.count = 0
		s.statusMsg = "Counter reset to 0."
		return true
	case s.docsCard.Contains(ev.X, ev.Y):
		s.statusMsg = "Docs: https://gopkin.coderscircle.in/docs.html"
		return true
	case s.settingsCrd.Contains(ev.X, ev.Y):
		s.dark = !s.dark
		if s.dark {
			s.statusMsg = "Settings: Dark Mode enabled."
		} else {
			s.statusMsg = "Settings: Light Mode enabled."
		}
		return true
	default:
		return false
	}
}
