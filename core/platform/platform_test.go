package platform

import "testing"

func TestMockDriverLifecycleAndDrawing(t *testing.T) {
	var drv Driver = NewMockDriver()
	mock := drv.(*MockDriver)

	if err := drv.CreateWindow("Early", 800, 600); err == nil {
		t.Fatal("expected error creating window before Init")
	}

	if err := drv.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if err := drv.CreateWindow("Gopkin App", 800, 600); err != nil {
		t.Fatalf("CreateWindow failed: %v", err)
	}

	drv.DrawRect(10, 20, 100, 40, ColorBlack)
	drv.DrawText(16, 30, "Hello", 16, ColorWhite)

	if len(mock.Rects) != 1 || mock.Rects[0].Color != ColorBlack {
		t.Fatalf("unexpected rect calls: %+v", mock.Rects)
	}
	if len(mock.Texts) != 1 || mock.Texts[0].Text != "Hello" {
		t.Fatalf("unexpected text calls: %+v", mock.Texts)
	}

	mock.PushEvent(Event{Type: EventClick, X: 25, Y: 35})
	events := drv.PollEvents()
	if len(events) != 1 || events[0].Type != EventClick {
		t.Fatalf("unexpected events: %+v", events)
	}
	if again := drv.PollEvents(); len(again) != 0 {
		t.Fatalf("expected empty event queue after poll, got %+v", again)
	}

	mock.MaxFrames = 5
	calls := 0
	if err := drv.RunLoop(func() bool {
		calls++
		return calls < 3
	}); err != nil {
		t.Fatalf("RunLoop failed: %v", err)
	}
	if calls != 3 || mock.FrameCount != 3 {
		t.Fatalf("expected 3 frames, got calls=%d FrameCount=%d", calls, mock.FrameCount)
	}
}

