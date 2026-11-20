package ui

import (
	"fmt"
	"math"
	"strconv"

	"gopkin/core/platform"
)

// vectorCanvas is optionally implemented by rich 2D drivers (Win32Driver, WebDriver)
// for crisp rounded cards, pill buttons, and isometric vector icons.
type vectorCanvas interface {
	DrawRoundedRect(x, y, w, h, radius float32, color platform.Color)
	DrawPolyline(points []platform.Point, width float32, color platform.Color)
}

// StarterScreenWidget renders the clean centered Gopkin starter UI:
//   - Top bar with purple isometric cube + "GOPKIN", centered app title, and dark/light pill toggle
//   - Centered large isometric cube, "Welcome to <AppName>", and "Built with GOPKIN"
//   - Centered rounded Counter card with "-", purple "+", and refresh icon buttons
//   - Centered Documentation pill card with book icon and chevron ">"
//   - Soft wave footer accent
type StarterScreenWidget struct {
	appName   string
	count     int
	dark      bool
	statusMsg string

	bounds      Rect
	themeBtn    Rect
	counterCard Rect
	decBtn      Rect
	incBtn      Rect
	resetBtn    Rect
	docsCard    Rect
}

// StarterScreen creates the interactive Gopkin starter UI for appName.
func StarterScreen(appName string) *StarterScreenWidget {
	if appName == "" {
		appName = "MyApp"
	}
	return &StarterScreenWidget{
		appName: appName,
	}
}

func (s *StarterScreenWidget) Type() string   { return "starter_screen" }
func (s *StarterScreenWidget) Bounds() Rect   { return s.bounds }
func (s *StarterScreenWidget) Count() int     { return s.count }
func (s *StarterScreenWidget) IsDark() bool   { return s.dark }
func (s *StarterScreenWidget) Status() string { return s.statusMsg }

func (s *StarterScreenWidget) Layout(bounds Rect) Rect {
	s.bounds = bounds

	const topBarH float32 = 52
	// Top bar right-aligned pill toggle button.
	const toggleW float32 = 48
	const toggleH float32 = 28
	s.themeBtn = Rect{
		X: bounds.X + max(16, bounds.W-toggleW-24),
		Y: bounds.Y + (topBarH-toggleH)/2,
		W: toggleW,
		H: toggleH,
	}

	centerX := bounds.X + bounds.W/2

	// Centered Counter card (~310px wide, 146px tall).
	cardW := min(float32(320), max(260, bounds.W-64))
	cardH := float32(146)
	cardY := bounds.Y + topBarH + 178
	s.counterCard = Rect{
		X: centerX - cardW/2,
		Y: cardY,
		W: cardW,
		H: cardH,
	}

	// 3 centered rounded buttons inside Counter card: "-", "+", "↻".
	const btnW float32 = 54
	const btnH float32 = 34
	const gap float32 = 12
	totalBtnsW := btnW*3 + gap*2
	btnStartX := centerX - totalBtnsW/2
	btnY := cardY + cardH - btnH - 18

	s.decBtn = Rect{X: btnStartX, Y: btnY, W: btnW, H: btnH}
	s.incBtn = Rect{X: btnStartX + btnW + gap, Y: btnY, W: btnW, H: btnH}
	s.resetBtn = Rect{X: btnStartX + (btnW+gap)*2, Y: btnY, W: btnW, H: btnH}

	// Centered Documentation pill card below Counter card.
	docsW := min(float32(240), max(200, bounds.W-80))
	docsH := float32(52)
	s.docsCard = Rect{
		X: centerX - docsW/2,
		Y: cardY + cardH + 26,
		W: docsW,
		H: docsH,
	}

	return s.bounds
}

func (s *StarterScreenWidget) Render(driver platform.Driver) {
	// Light / Dark theme palette matching Gopkin's modern purple-accent starter UI.
	accent := platform.Color{R: 0x6D, G: 0x28, B: 0xD9, A: 0xFF}      // #6D28D9
	accentSoft := platform.Color{R: 0xF3, G: 0xF0, B: 0xFF, A: 0xFF}  // #F3F0FF
	bg := platform.ColorWhite                                          // #FFFFFF
	topBg := platform.Color{R: 0xFC, G: 0xFC, B: 0xFE, A: 0xFF}       // #FCFCFE
	cardBg := platform.Color{R: 0xFA, G: 0xF9, B: 0xFD, A: 0xFF}      // #FAF9FD
	btnBg := platform.ColorWhite                                       // #FFFFFF
	pillBg := platform.Color{R: 0xEB, G: 0xEE, B: 0xF5, A: 0xFF}      // #EBEEF5
	border := platform.Color{R: 0xE5, G: 0xE7, B: 0xEB, A: 0xFF}      // #E5E7EB
	textPrimary := platform.Color{R: 0x11, G: 0x18, B: 0x27, A: 0xFF} // #111827
	textMuted := platform.Color{R: 0x6B, G: 0x72, B: 0x80, A: 0xFF}   // #6B7280

	if s.dark {
		accent = platform.Color{R: 0x8B, G: 0x5C, B: 0xF6, A: 0xFF}
		accentSoft = platform.Color{R: 0x1A, G: 0x16, B: 0x2E, A: 0xFF}
		bg = platform.Color{R: 0x0B, G: 0x0D, B: 0x14, A: 0xFF}
		topBg = platform.Color{R: 0x10, G: 0x13, B: 0x1D, A: 0xFF}
		cardBg = platform.Color{R: 0x15, G: 0x19, B: 0x26, A: 0xFF}
		btnBg = platform.Color{R: 0x1C, G: 0x21, B: 0x32, A: 0xFF}
		pillBg = platform.Color{R: 0x1F, G: 0x25, B: 0x38, A: 0xFF}
		border = platform.Color{R: 0x2A, G: 0x31, B: 0x45, A: 0xFF}
		textPrimary = platform.Color{R: 0xF8, G: 0xFA, B: 0xFC, A: 0xFF}
		textMuted = platform.Color{R: 0x9C, G: 0xA3, B: 0xAF, A: 0xFF}
	}

	vc, hasVec := driver.(vectorCanvas)
	fillRounded := func(r Rect, radius float32, c platform.Color) {
		if hasVec {
			vc.DrawRoundedRect(r.X, r.Y, r.W, r.H, radius, c)
		} else {
			driver.DrawRect(r.X, r.Y, r.W, r.H, c)
		}
	}
	fillBorderedRounded := func(r Rect, radius float32, bdr, fill platform.Color) {
		fillRounded(r, radius, bdr)
		inner := Rect{X: r.X + 1, Y: r.Y + 1, W: max(0, r.W-2), H: max(0, r.H-2)}
		fillRounded(inner, max(0, radius-1), fill)
	}

	// 1. Full-viewport background + subtle bottom wave accent.
	driver.DrawRect(s.bounds.X, s.bounds.Y, s.bounds.W, s.bounds.H, bg)
	drawBottomWave(driver, s.bounds, accentSoft)

	// 2. Top Navigation Bar (52px).
	const topBarH float32 = 52
	driver.DrawRect(s.bounds.X, s.bounds.Y, s.bounds.W, topBarH, topBg)
	driver.DrawRect(s.bounds.X, s.bounds.Y+topBarH-1, s.bounds.W, 1, border)

	// Top-left isometric cube icon + "GOPKIN".
	drawCubeIcon(driver, vc, s.bounds.X+22, s.bounds.Y+14, 24, accent, 2.2)
	driver.DrawText(s.bounds.X+54, s.bounds.Y+16, "GOPKIN", 16, textPrimary)

	// Top-center app title.
	centerX := s.bounds.X + s.bounds.W/2
	titleW := float32(len(s.appName)) * 8.5
	driver.DrawText(centerX-titleW/2, s.bounds.Y+16, s.appName, 15, textPrimary)

	// Top-right theme pill toggle with moon/sun icon.
	fillRounded(s.themeBtn, 14, pillBg)
	drawThemeIcon(driver, vc, s.themeBtn.X+s.themeBtn.W/2, s.themeBtn.Y+s.themeBtn.H/2, s.dark, textPrimary, pillBg)

	// 3. Centered Hero: large isometric cube + "Welcome to <AppName>" + "Built with GOPKIN".
	heroTop := s.bounds.Y + topBarH + 34
	drawCubeIcon(driver, vc, centerX-26, heroTop, 52, accent, 4.0)

	prefix := "Welcome to "
	prefixW := float32(len(prefix)) * 14.2
	appNameW := float32(len(s.appName)) * 14.5
	totalHeroW := prefixW + appNameW
	heroTextX := centerX - totalHeroW/2
	heroTextY := heroTop + 68
	driver.DrawText(heroTextX, heroTextY, prefix, 28, textPrimary)
	driver.DrawText(heroTextX+prefixW, heroTextY, s.appName, 28, accent)

	sub := "Built with GOPKIN"
	subW := float32(len(sub)) * 7.6
	driver.DrawText(centerX-subW/2, heroTextY+40, sub, 14, textMuted)

	// 4. Centered Rounded Counter Card.
	fillBorderedRounded(s.counterCard, 16, border, cardBg)

	cntLabel := "Counter"
	driver.DrawText(centerX-float32(len(cntLabel))*4.0, s.counterCard.Y+16, cntLabel, 14, textMuted)

	cntStr := strconv.Itoa(s.count)
	cntW := float32(len(cntStr)) * 18
	driver.DrawText(centerX-cntW/2, s.counterCard.Y+42, cntStr, 36, accent)

	// Counter Buttons: "-", purple "+", refresh icon.
	fillBorderedRounded(s.decBtn, 10, border, btnBg)
	driver.DrawText(s.decBtn.X+s.decBtn.W/2-4, s.decBtn.Y+7, "-", 18, textPrimary)

	fillRounded(s.incBtn, 10, accent)
	driver.DrawText(s.incBtn.X+s.incBtn.W/2-5, s.incBtn.Y+7, "+", 18, platform.ColorWhite)

	fillBorderedRounded(s.resetBtn, 10, border, btnBg)
	drawRefreshIcon(driver, vc, s.resetBtn.X+s.resetBtn.W/2, s.resetBtn.Y+s.resetBtn.H/2, textPrimary)

	// 5. Centered Documentation Pill Card.
	fillBorderedRounded(s.docsCard, 12, border, btnBg)
	drawBookIcon(driver, vc, s.docsCard.X+22, s.docsCard.Y+17, accent)
	driver.DrawText(s.docsCard.X+52, s.docsCard.Y+17, "Documentation", 15, textPrimary)
	drawChevronRight(driver, vc, s.docsCard.X+s.docsCard.W-24, s.docsCard.Y+s.docsCard.H/2, textMuted)

	// Optional live feedback toast line below Documentation card when clicked.
	if s.statusMsg != "" {
		msgW := float32(len(s.statusMsg)) * 6.8
		driver.DrawText(centerX-msgW/2, s.docsCard.Y+s.docsCard.H+16, s.statusMsg, 12, textMuted)
	}
}

func (s *StarterScreenWidget) HandleEvent(ev platform.Event) bool {
	if ev.Type == platform.EventKeyDown {
		switch ev.Key {
		case platform.KeyUp:
			s.count++
			return true
		case platform.KeyDown:
			s.count--
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
			s.statusMsg = "Dark mode enabled"
		} else {
			s.statusMsg = "Light mode enabled"
		}
		return true
	case s.incBtn.Contains(ev.X, ev.Y):
		s.count++
		s.statusMsg = ""
		return true
	case s.decBtn.Contains(ev.X, ev.Y):
		s.count--
		s.statusMsg = ""
		return true
	case s.resetBtn.Contains(ev.X, ev.Y):
		s.count = 0
		s.statusMsg = "Counter reset to 0"
		return true
	case s.docsCard.Contains(ev.X, ev.Y):
		s.statusMsg = fmt.Sprintf("Docs: https://gopkin.coderscircle.in/docs.html")
		return true
	default:
		return false
	}
}

// --- Vector Icon Helpers (Pure-Go primitives for Win32 + Web + Mock) ---

func drawCubeIcon(driver platform.Driver, vc vectorCanvas, x, y, size float32, color platform.Color, stroke float32) {
	if vc == nil {
		driver.DrawRect(x, y, size, size, color)
		return
	}
	cx := x + size/2
	cy := y + size/2
	r := size * 0.48
	dx := r * 0.866
	dy := r * 0.5

	top := platform.Point{X: cx, Y: cy - r}
	tr := platform.Point{X: cx + dx, Y: cy - dy}
	br := platform.Point{X: cx + dx, Y: cy + dy}
	bot := platform.Point{X: cx, Y: cy + r}
	bl := platform.Point{X: cx - dx, Y: cy + dy}
	tl := platform.Point{X: cx - dx, Y: cy - dy}
	mid := platform.Point{X: cx, Y: cy}

	// Outer hexagon
	vc.DrawPolyline([]platform.Point{top, tr, br, bot, bl, tl, top}, stroke, color)
	// Inner Y edges
	vc.DrawPolyline([]platform.Point{tl, mid, tr}, stroke, color)
	vc.DrawPolyline([]platform.Point{mid, bot}, stroke, color)
}

func drawThemeIcon(driver platform.Driver, vc vectorCanvas, cx, cy float32, dark bool, fg, bg platform.Color) {
	if vc == nil {
		label := "D"
		if dark {
			label = "L"
		}
		driver.DrawText(cx-4, cy-7, label, 12, fg)
		return
	}
	if dark {
		// Sun icon in dark mode
		vc.DrawRoundedRect(cx-4, cy-4, 8, 8, 4, fg)
		for i := 0; i < 8; i++ {
			ang := float64(i) * math.Pi / 4
			x1 := cx + float32(math.Cos(ang)*6.5)
			y1 := cy + float32(math.Sin(ang)*6.5)
			x2 := cx + float32(math.Cos(ang)*9)
			y2 := cy + float32(math.Sin(ang)*9)
			vc.DrawPolyline([]platform.Point{{X: x1, Y: y1}, {X: x2, Y: y2}}, 1.5, fg)
		}
		return
	}
	// Crescent moon icon in light mode
	vc.DrawRoundedRect(cx-6, cy-6, 12, 12, 6, fg)
	vc.DrawRoundedRect(cx-2, cy-7, 10, 10, 5, bg)
}

func drawRefreshIcon(driver platform.Driver, vc vectorCanvas, cx, cy float32, color platform.Color) {
	if vc == nil {
		driver.DrawText(cx-5, cy-8, "R", 14, color)
		return
	}
	var pts []platform.Point
	const r = 6.5
	for deg := -35; deg <= 255; deg += 25 {
		rad := float64(deg) * math.Pi / 180
		pts = append(pts, platform.Point{
			X: cx + float32(math.Cos(rad)*r),
			Y: cy + float32(math.Sin(rad)*r),
		})
	}
	vc.DrawPolyline(pts, 1.8, color)
	// Small arrowhead at top-right of arc
	vc.DrawPolyline([]platform.Point{
		{X: cx + 2, Y: cy - 7},
		{X: cx + 6, Y: cy - 3.5},
		{X: cx + 1.5, Y: cy - 2.5},
	}, 1.8, color)
}

func drawBookIcon(driver platform.Driver, vc vectorCanvas, x, y float32, color platform.Color) {
	if vc == nil {
		driver.DrawRect(x, y, 16, 14, color)
		return
	}
	// Left page
	vc.DrawPolyline([]platform.Point{
		{X: x, Y: y + 2}, {X: x + 9, Y: y + 4}, {X: x + 9, Y: y + 17}, {X: x, Y: y + 15}, {X: x, Y: y + 2},
	}, 1.8, color)
	// Right page
	vc.DrawPolyline([]platform.Point{
		{X: x + 9, Y: y + 4}, {X: x + 18, Y: y + 2}, {X: x + 18, Y: y + 15}, {X: x + 9, Y: y + 17},
	}, 1.8, color)
}

func drawChevronRight(driver platform.Driver, vc vectorCanvas, cx, cy float32, color platform.Color) {
	if vc == nil {
		driver.DrawText(cx-3, cy-8, ">", 14, color)
		return
	}
	vc.DrawPolyline([]platform.Point{
		{X: cx - 3, Y: cy - 5},
		{X: cx + 3, Y: cy},
		{X: cx - 3, Y: cy + 5},
	}, 1.8, color)
}

func drawBottomWave(driver platform.Driver, bounds Rect, waveColor platform.Color) {
	if bounds.W <= 0 || bounds.H <= 80 {
		return
	}
	const steps = 40
	stepW := bounds.W/float32(steps) + 1
	baseY := bounds.Y + bounds.H
	for i := 0; i < steps; i++ {
		t := float64(i) / float64(steps)
		waveH := float32(34 + 20*math.Sin(t*math.Pi*2+0.6))
		driver.DrawRect(bounds.X+float32(i)*bounds.W/float32(steps), baseY-waveH, stepW, waveH, waveColor)
	}
}
