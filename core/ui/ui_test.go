package ui

import (
	"testing"

	"gopkin/core/platform"
	"gopkin/core/state"
)

func TestHeadingAndReactiveText(t *testing.T) {
	drv := platform.NewMockDriver()
	msg := state.NewState("Hello")
	count := state.NewNumState(3)

	h := Heading("Gopkin")
	t1 := Text(msg)
	t2 := Text(count).Muted()

	col := Column(h, t1, t2)
	col.Layout(Rect{X: 0, Y: 0, W: 400, H: 300})
	col.Render(drv)

	if len(drv.Texts) != 3 {
		t.Fatalf("expected 3 text draws, got %d", len(drv.Texts))
	}
	if drv.Texts[0].Text != "Gopkin" || drv.Texts[0].Color != platform.ColorBlack {
		t.Fatalf("unexpected heading draw: %+v", drv.Texts[0])
	}
	if drv.Texts[1].Text != "Hello" {
		t.Fatalf("expected reactive text 'Hello', got %q", drv.Texts[1].Text)
	}
	if drv.Texts[2].Text != "3" || drv.Texts[2].Color != platform.ColorAccentGrey {
		t.Fatalf("unexpected muted num text: %+v", drv.Texts[2])
	}

	msg.Set("Updated")
	count.Inc()
	if t1.Content() != "Updated" || t2.Content() != "4" {
		t.Fatalf("expected updated reactive content, got %q and %q", t1.Content(), t2.Content())
	}
}

func TestButtonClickDoubleClickAndFullWidth(t *testing.T) {
	clicked := 0
	dblClicked := 0

	b1 := Btn("Save", func() { clicked++ }).OnDoubleClick(func() { dblClicked++ })
	b2 := Btn("Wide", nil).FullWidth()

	r1 := b1.Layout(Rect{X: 10, Y: 10, W: 400, H: 100})
	r2 := b2.Layout(Rect{X: 10, Y: 60, W: 400, H: 100})

	if r1.W >= 400 {
		t.Fatalf("expected non-full-width button to be narrower than 400, got %v", r1.W)
	}
	if r2.W != 400 || !b2.IsFullWidth() {
		t.Fatalf("expected full-width button to span 400, got %v", r2.W)
	}

	// Click outside does nothing.
	if b1.HandleEvent(platform.Event{Type: platform.EventClick, X: 350, Y: 20}) {
		t.Fatal("expected click outside button to be ignored")
	}

	// Click inside fires onClick.
	if !b1.HandleEvent(platform.Event{Type: platform.EventClick, X: 25, Y: 25}) {
		t.Fatal("expected click inside button to be consumed")
	}
	if clicked != 1 {
		t.Fatalf("expected clicked=1, got %d", clicked)
	}

	// Double-click inside fires onDoubleClick.
	if !b1.HandleEvent(platform.Event{Type: platform.EventDoubleClick, X: 25, Y: 25}) {
		t.Fatal("expected double-click inside button to be consumed")
	}
	if dblClicked != 1 {
		t.Fatalf("expected dblClicked=1, got %d", dblClicked)
	}

	drv := platform.NewMockDriver()
	b1.Render(drv)
	if len(drv.Rects) != 1 || drv.Rects[0].Color != platform.ColorBlack {
		t.Fatalf("expected monochrome black button background, got %+v", drv.Rects)
	}
	if len(drv.Texts) != 1 || drv.Texts[0].Color != platform.ColorWhite || drv.Texts[0].Text != "Save" {
		t.Fatalf("expected monochrome white button label, got %+v", drv.Texts)
	}
}

func TestLayoutsRowColumnCenterGrid(t *testing.T) {
	// Row splits width evenly.
	b1 := Btn("A").FullWidth()
	b2 := Btn("B").FullWidth()
	row := Row(b1, b2).Gap(10)
	row.Layout(Rect{X: 0, Y: 0, W: 210, H: 100})
	if b1.Bounds().W != 100 || b2.Bounds().W != 100 || b2.Bounds().X != 110 {
		t.Fatalf("unexpected row layout: b1=%+v b2=%+v", b1.Bounds(), b2.Bounds())
	}

	// Column stacks vertically.
	t1 := Text("Line 1")
	t2 := Text("Line 2")
	col := Column(t1, t2).Gap(6)
	col.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})
	if t2.Bounds().Y != t1.Bounds().H+6 {
		t.Fatalf("expected t2.Y=%v, got %v", t1.Bounds().H+6, t2.Bounds().Y)
	}

	// Center positions child in the middle.
	cb := Btn("OK")
	center := Center(cb)
	center.Layout(Rect{X: 0, Y: 0, W: 400, H: 200})
	expectedX := (400 - cb.Bounds().W) / 2
	expectedY := (200 - cb.Bounds().H) / 2
	if cb.Bounds().X != expectedX || cb.Bounds().Y != expectedY {
		t.Fatalf("expected centered (%v, %v), got (%v, %v)", expectedX, expectedY, cb.Bounds().X, cb.Bounds().Y)
	}

	// Grid places items into columns and rows.
	g1, g2, g3 := Btn("1").FullWidth(), Btn("2").FullWidth(), Btn("3").FullWidth()
	grid := Grid(2, g1, g2, g3).Gap(10)
	grid.Layout(Rect{X: 0, Y: 0, W: 210, H: 300})
	if g1.Bounds().X != 0 || g2.Bounds().X != 110 || g3.Bounds().X != 0 {
		t.Fatalf("unexpected grid X coords: g1=%+v g2=%+v g3=%+v", g1.Bounds(), g2.Bounds(), g3.Bounds())
	}
	if g3.Bounds().Y <= g1.Bounds().Y {
		t.Fatalf("expected g3 on second row below g1: g1=%+v g3=%+v", g1.Bounds(), g3.Bounds())
	}
}

func TestCardAndSlider(t *testing.T) {
	vol := state.NewNumState(50)
	var changed float64
	sl := Slider(0, 100, vol).OnChange(func(v float64) { changed = v })

	card := Card(Heading("Audio"), sl)
	card.Layout(Rect{X: 0, Y: 0, W: 232, H: 300})

	// Slider inside card (padding 16) spans X=16..216 (W=200). Clicking midpoint X=116 -> 50, X=166 -> 75.
	sb := sl.Bounds()
	if !card.HandleEvent(platform.Event{Type: platform.EventClick, X: sb.X + sb.W*0.75, Y: sb.Y + sb.H/2}) {
		t.Fatal("expected slider click to be consumed")
	}
	if vol.Val() != 75 || changed != 75 {
		t.Fatalf("expected slider value 75, got state=%v changed=%v", vol.Val(), changed)
	}

	drv := platform.NewMockDriver()
	card.Render(drv)
	if len(drv.Rects) < 5 {
		t.Fatalf("expected card border/bg + slider track/fill/thumb rects, got %d", len(drv.Rects))
	}
	if drv.Rects[0].Color != platform.ColorAccentGrey || drv.Rects[1].Color != platform.ColorSurfaceLight {
		t.Fatalf("expected monochrome card colors, got %+v and %+v", drv.Rects[0].Color, drv.Rects[1].Color)
	}
}

func TestPopupWidget(t *testing.T) {
	closed := false
	okClicked := false
	pop := Popup(
		"Success",
		"Action completed!",
		Btn("OK", func() { okClicked = true }),
		func() { closed = true },
	)

	if !pop.IsOpen() || pop.Title() != "Success" {
		t.Fatalf("unexpected popup initial state: open=%v title=%q", pop.IsOpen(), pop.Title())
	}
	if len(pop.Children()) != 2 {
		t.Fatalf("expected 2 children (Text + Btn), got %d", len(pop.Children()))
	}

	pop.Layout(Rect{X: 0, Y: 0, W: 800, H: 600})
	drv := platform.NewMockDriver()
	pop.Render(drv)

	if len(drv.Texts) < 3 {
		t.Fatalf("expected title, body, and button text draws, got %d", len(drv.Texts))
	}

	okBtn := pop.Children()[1].(*ButtonWidget)
	if !pop.HandleEvent(platform.Event{Type: platform.EventClick, X: okBtn.Bounds().X + 5, Y: okBtn.Bounds().Y + 5}) {
		t.Fatal("expected popup child button click to be consumed")
	}
	if !okClicked {
		t.Fatal("expected OK button callback to fire")
	}

	pop.Close()
	if pop.IsOpen() || !closed {
		t.Fatalf("expected popup closed and onClose fired, open=%v closed=%v", pop.IsOpen(), closed)
	}

	drv.ResetDrawCalls()
	pop.Render(drv)
	if len(drv.Rects) != 0 || len(drv.Texts) != 0 {
		t.Fatalf("expected closed popup to render nothing, got rects=%d texts=%d", len(drv.Rects), len(drv.Texts))
	}
}

