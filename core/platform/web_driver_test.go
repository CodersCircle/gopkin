package platform

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebDriverIndexHTMLAndSEO(t *testing.T) {
	drv := NewWebDriver("127.0.0.1:8080").SetMeta(WebMeta{
		Title:       "Gopkin Dashboard",
		Description: "Monochrome Pure-Go IoT Dashboard",
		AppID:       "com.gopkin.dashboard",
		Version:     "1.0.0",
	})
	if err := drv.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if err := drv.CreateWindow("Gopkin Dashboard", 1024, 768); err != nil {
		t.Fatalf("CreateWindow failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	drv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	wantSnippets := []string{
		"<!DOCTYPE html>",
		"<title>Gopkin Dashboard</title>",
		`content="Monochrome Pure-Go IoT Dashboard"`,
		`content="com.gopkin.dashboard"`,
		`<canvas id="gopkin-canvas"`,
		"window.devicePixelRatio",
		"EventSource",
	}
	for _, snip := range wantSnippets {
		if !strings.Contains(body, snip) {
			t.Errorf("GET / HTML missing %q", snip)
		}
	}
}

func TestWebDriverFrameJSONDrawCommands(t *testing.T) {
	drv := NewWebDriver("")
	if err := drv.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if err := drv.CreateWindow("Canvas Test", 800, 600); err != nil {
		t.Fatalf("CreateWindow failed: %v", err)
	}

	drv.RenderOnce(func() bool {
		drv.Clear(ColorWhite)
		drv.FillRect(10, 20, 200, 48, ColorBlack)
		drv.DrawRoundedRect(10, 80, 120, 36, 8, ColorSurfaceLight)
		drv.DrawText(24, 34, "Hello Web", 16, ColorWhite)
		drv.DrawPolyline([]Point{{X: 0, Y: 100}, {X: 50, Y: 75}, {X: 100, Y: 120}}, 2, ColorBlack)
		return true
	})

	req := httptest.NewRequest(http.MethodGet, "/frame", nil)
	rec := httptest.NewRecorder()
	drv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /frame status = %d, want 200", rec.Code)
	}

	var frame WebFrame
	if err := json.Unmarshal(rec.Body.Bytes(), &frame); err != nil {
		t.Fatalf("decode /frame JSON failed: %v", err)
	}
	if frame.Width != 800 || frame.Height != 600 {
		t.Fatalf("unexpected frame dimensions: %dx%d", frame.Width, frame.Height)
	}
	if len(frame.Commands) != 5 {
		t.Fatalf("expected 5 draw commands, got %d: %+v", len(frame.Commands), frame.Commands)
	}

	if frame.Commands[0].Op != "clear" || frame.Commands[0].Color != "#ffffff" {
		t.Errorf("unexpected cmd[0]: %+v", frame.Commands[0])
	}
	if frame.Commands[1].Op != "fillRect" || frame.Commands[1].W != 200 || frame.Commands[1].H != 48 {
		t.Errorf("unexpected cmd[1] FillRect: %+v", frame.Commands[1])
	}
	if frame.Commands[2].Op != "roundedRect" || frame.Commands[2].Radius != 8 {
		t.Errorf("unexpected cmd[2] DrawRoundedRect: %+v", frame.Commands[2])
	}
	if frame.Commands[3].Op != "text" || frame.Commands[3].Text != "Hello Web" {
		t.Errorf("unexpected cmd[3] DrawText: %+v", frame.Commands[3])
	}
	if frame.Commands[4].Op != "polyline" || len(frame.Commands[4].Points) != 3 || frame.Commands[4].Width != 2 {
		t.Errorf("unexpected cmd[4] DrawPolyline: %+v", frame.Commands[4])
	}
}

func TestWebDriverPostEventTranslation(t *testing.T) {
	drv := NewWebDriver("")
	if err := drv.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	handler := drv.Handler()

	postEvent := func(payload string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/event", bytes.NewBufferString(payload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /event (%s) status = %d, want 200", payload, rec.Code)
		}
	}

	postEvent(`{"type":"click","x":120,"y":64}`)
	postEvent(`{"type":"keydown","key":"Backspace"}`)
	postEvent(`{"type":"keydown","key":"Tab","shift":true}`)
	postEvent(`{"type":"char","char":"G"}`)
	postEvent(`{"type":"resize","width":1280,"height":720}`)

	events := drv.PollEvents()
	if len(events) != 5 {
		t.Fatalf("expected 5 polled events, got %d: %+v", len(events), events)
	}

	if events[0].Type != EventClick || events[0].X != 120 || events[0].Y != 64 {
		t.Errorf("unexpected click event: %+v", events[0])
	}
	if events[1].Type != EventKeyDown || events[1].Key != KeyBackspace {
		t.Errorf("unexpected keydown Backspace event: %+v", events[1])
	}
	if events[2].Type != EventKeyDown || events[2].Key != KeyTab || !events[2].Shift {
		t.Errorf("unexpected Shift+Tab event: %+v", events[2])
	}
	if events[3].Type != EventChar || events[3].Rune != 'G' {
		t.Errorf("unexpected char event: %+v", events[3])
	}
	if events[4].Type != EventResize || events[4].Width != 1280 || events[4].Height != 720 {
		t.Errorf("unexpected resize event: %+v", events[4])
	}
}
