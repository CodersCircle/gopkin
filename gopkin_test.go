package gopkin_test

import (
	"testing"

	. "gopkin"
	"gopkin/core/platform"
)

func TestDotImportAndRunApp(t *testing.T) {
	count := Num(0)
	status := State("ready")
	dblClicks := 0

	incBtn := Btn("+", count.Inc).OnDoubleClick(func() { dblClicks++ })
	resetBtn := Btn("Reset", count.SetTo(0)).FullWidth()
	slider := Slider(0, 10, count)
	popup := Popup("Notice", "Saved!")

	app := Center(
		Card(
			Heading("Counter"),
			Text(count),
			Text(status),
			Row(incBtn, Btn("-", count.Dec)),
			Grid(2, resetBtn, slider),
			popup,
		),
	)

	drv := platform.NewMockDriver()
	drv.MaxFrames = 2

	if err := RunWithDriver(drv, "Gopkin Demo", app); err != nil {
		t.Fatalf("RunWithDriver failed: %v", err)
	}

	if drv.WindowTitle != "Gopkin Demo" {
		t.Fatalf("expected window title 'Gopkin Demo', got %q", drv.WindowTitle)
	}
	if drv.FrameCount != 2 {
		t.Fatalf("expected 2 rendered frames, got %d", drv.FrameCount)
	}
	if len(drv.Texts) == 0 || len(drv.Rects) == 0 {
		t.Fatalf("expected draw calls to be recorded, got rects=%d texts=%d", len(drv.Rects), len(drv.Texts))
	}

	// Verify event dispatch through RunWithDriver triggers reactive state changes.
	drv2 := platform.NewMockDriver()
	drv2.MaxFrames = 2
	clickBtn := Btn("Add", count.Inc).FullWidth()
	drv2.PushEvent(platform.Event{Type: platform.EventClick, X: 20, Y: 20})
	drv2.PushEvent(platform.Event{Type: platform.EventQuit})

	if err := RunWithDriver(drv2, "Event Test", clickBtn); err != nil {
		t.Fatalf("RunWithDriver event test failed: %v", err)
	}
	if count.Val() != 1 {
		t.Fatalf("expected count to increment to 1 on click, got %v", count.Val())
	}
	if drv2.FrameCount != 1 {
		t.Fatalf("expected loop to stop on EventQuit at frame 1, got %d", drv2.FrameCount)
	}
	if dblClicks != 0 {
		t.Fatalf("expected 0 double clicks, got %d", dblClicks)
	}
}

func TestDefaultRunUsesMockDriver(t *testing.T) {
	if err := Run("Default Run", Heading("Hi"), Text("Zero deps")); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
}

