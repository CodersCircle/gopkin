package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DrawCommand represents a single serialized 2D drawing operation sent to the
// browser HTML5 <canvas> renderer.
type DrawCommand struct {
	Op     string  `json:"op"`
	X      float32 `json:"x,omitempty"`
	Y      float32 `json:"y,omitempty"`
	W      float32 `json:"w,omitempty"`
	H      float32 `json:"h,omitempty"`
	Radius float32 `json:"radius,omitempty"`
	Text   string  `json:"text,omitempty"`
	Size   float32 `json:"size,omitempty"`
	Width  float32 `json:"width,omitempty"`
	Color  string  `json:"color,omitempty"`
	Points []Point `json:"points,omitempty"`
}

// WebFrame is the JSON payload streamed to connected browser clients.
type WebFrame struct {
	Seq      uint64        `json:"seq"`
	Width    int           `json:"width"`
	Height   int           `json:"height"`
	Commands []DrawCommand `json:"commands"`
}

// WebMeta holds SEO and document metadata populated from gopkin.yaml.
type WebMeta struct {
	Title       string
	Description string
	AppID       string
	Version     string
}

// WebDriver implements Driver over Go stdlib net/http, rendering drawing
// commands to a HiDPI HTML5 <canvas> and streaming updates via SSE while
// forwarding browser input events over POST /event.
type WebDriver struct {
	mu          sync.Mutex
	addr        string
	meta        WebMeta
	initialized bool
	width       int
	height      int
	pending     []DrawCommand
	lastFrame   WebFrame
	lastJSON    []byte
	events      []Event
	wakeCh      chan struct{}
	stopCh      chan struct{}
	stopOnce    sync.Once

	subMu       sync.Mutex
	subscribers map[chan []byte]struct{}

	server   *http.Server
	listener net.Listener
}

// NewWebDriver creates a new Live Web Target Driver bound to addr.
// If addr is empty, it defaults to "127.0.0.1:8080".
func NewWebDriver(addr string) *WebDriver {
	if strings.TrimSpace(addr) == "" {
		addr = "127.0.0.1:8080"
	}
	return &WebDriver{
		addr:        addr,
		width:       800,
		height:      600,
		wakeCh:      make(chan struct{}, 1),
		stopCh:      make(chan struct{}),
		subscribers: make(map[chan []byte]struct{}),
		meta: WebMeta{
			Title:       "Gopkin App",
			Description: "Built with Gopkin — Pure-Go Cross-Platform Framework",
		},
	}
}

// SetMeta configures SEO title, description, app ID, and version for the HTML5 shell.
func (w *WebDriver) SetMeta(meta WebMeta) *WebDriver {
	w.mu.Lock()
	if meta.Title != "" {
		w.meta.Title = meta.Title
	}
	if meta.Description != "" {
		w.meta.Description = meta.Description
	}
	if meta.AppID != "" {
		w.meta.AppID = meta.AppID
	}
	if meta.Version != "" {
		w.meta.Version = meta.Version
	}
	w.mu.Unlock()
	return w
}

// Addr returns the configured or bound listen address.
func (w *WebDriver) Addr() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.listener != nil {
		return w.listener.Addr().String()
	}
	return w.addr
}

// URL returns the live HTTP URL of the web server.
func (w *WebDriver) URL() string {
	addr := w.Addr()
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	return "http://" + addr
}

// Init prepares the WebDriver for serving and rendering.
func (w *WebDriver) Init() error {
	w.mu.Lock()
	w.initialized = true
	w.mu.Unlock()
	return nil
}

// CreateWindow sets the logical canvas viewport dimensions and page title.
func (w *WebDriver) CreateWindow(title string, width, height int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.initialized {
		return errors.New("platform/web: driver not initialized")
	}
	if width <= 0 || height <= 0 {
		return errors.New("platform/web: invalid window dimensions")
	}
	if title != "" {
		w.meta.Title = title
	}
	w.width = width
	w.height = height
	return nil
}

// Clear appends a full-canvas clear command.
func (w *WebDriver) Clear(color Color) {
	w.appendCmd(DrawCommand{
		Op:    "clear",
		Color: colorToCSS(color),
	})
}

// FillRect appends a filled rectangle drawing command.
func (w *WebDriver) FillRect(x, y, width, height float32, color Color) {
	w.appendCmd(DrawCommand{
		Op:    "fillRect",
		X:     x,
		Y:     y,
		W:     width,
		H:     height,
		Color: colorToCSS(color),
	})
}

// DrawRect implements Driver.DrawRect by emitting a filled rectangle command.
func (w *WebDriver) DrawRect(x, y, width, height float32, color Color) {
	w.FillRect(x, y, width, height, color)
}

// DrawRoundedRect appends a filled rounded-rectangle drawing command.
func (w *WebDriver) DrawRoundedRect(x, y, width, height, radius float32, color Color) {
	w.appendCmd(DrawCommand{
		Op:     "roundedRect",
		X:      x,
		Y:      y,
		W:      width,
		H:      height,
		Radius: radius,
		Color:  colorToCSS(color),
	})
}

// DrawText implements Driver.DrawText by emitting a text drawing command.
func (w *WebDriver) DrawText(x, y float32, text string, size float32, color Color) {
	w.appendCmd(DrawCommand{
		Op:    "text",
		X:     x,
		Y:     y,
		Text:  text,
		Size:  size,
		Color: colorToCSS(color),
	})
}

// DrawPolyline appends a multi-segment polyline drawing command.
func (w *WebDriver) DrawPolyline(points []Point, lineWidth float32, color Color) {
	if len(points) == 0 {
		return
	}
	cp := make([]Point, len(points))
	copy(cp, points)
	if lineWidth <= 0 {
		lineWidth = 1
	}
	w.appendCmd(DrawCommand{
		Op:     "polyline",
		Width:  lineWidth,
		Color:  colorToCSS(color),
		Points: cp,
	})
}

func (w *WebDriver) appendCmd(cmd DrawCommand) {
	w.mu.Lock()
	w.pending = append(w.pending, cmd)
	w.mu.Unlock()
}

// BeginFrame resets the pending command buffer for a new frame.
func (w *WebDriver) BeginFrame() {
	w.mu.Lock()
	w.pending = w.pending[:0]
	w.mu.Unlock()
}

// CommitFrame snapshots pending draw commands into the active WebFrame and
// broadcasts the JSON payload to all connected SSE clients.
func (w *WebDriver) CommitFrame() WebFrame {
	w.mu.Lock()
	cmds := make([]DrawCommand, len(w.pending))
	copy(cmds, w.pending)
	w.pending = w.pending[:0]

	frame := WebFrame{
		Seq:      w.lastFrame.Seq + 1,
		Width:    w.width,
		Height:   w.height,
		Commands: cmds,
	}
	data, _ := json.Marshal(frame)
	w.lastFrame = frame
	w.lastJSON = data
	w.mu.Unlock()

	w.broadcast(data)
	return frame
}

// LastFrame returns the most recently committed WebFrame.
func (w *WebDriver) LastFrame() WebFrame {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastFrame
}

// RenderOnce executes a single frame callback wrapped in BeginFrame/CommitFrame.
func (w *WebDriver) RenderOnce(onFrame func() bool) bool {
	w.BeginFrame()
	keepRunning := true
	if onFrame != nil {
		keepRunning = onFrame()
	}
	w.CommitFrame()
	return keepRunning
}

// PollEvents implements Driver.PollEvents, returning and clearing queued browser events.
func (w *WebDriver) PollEvents() []Event {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.events) == 0 {
		return nil
	}
	out := make([]Event, len(w.events))
	copy(out, w.events)
	w.events = nil
	return out
}

func (w *WebDriver) pushEvent(ev Event) {
	w.mu.Lock()
	if ev.Type == EventResize && ev.Width > 0 && ev.Height > 0 {
		w.width = ev.Width
		w.height = ev.Height
	}
	w.events = append(w.events, ev)
	w.mu.Unlock()

	select {
	case w.wakeCh <- struct{}{}:
	default:
	}
}

// Close gracefully shuts down the HTTP server and terminates RunLoop.
func (w *WebDriver) Close() error {
	w.stopOnce.Do(func() {
		close(w.stopCh)
	})
	w.mu.Lock()
	srv := w.server
	w.mu.Unlock()
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
	return nil
}

// RunLoop starts the stdlib net/http server and drives onFrame reactively as
// browser events arrive until onFrame returns false or Close is called.
func (w *WebDriver) RunLoop(onFrame func() bool) error {
	w.mu.Lock()
	if !w.initialized {
		w.mu.Unlock()
		return errors.New("platform/web: driver not initialized")
	}
	addr := w.addr
	w.mu.Unlock()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("platform/web: listen %s: %w", addr, err)
	}

	srv := &http.Server{
		Handler:           w.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	w.mu.Lock()
	w.listener = ln
	w.server = srv
	w.mu.Unlock()

	// Render initial frame before serving clients.
	if !w.RenderOnce(onFrame) {
		_ = ln.Close()
		return nil
	}

	srvErrCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErrCh <- err
		}
		close(srvErrCh)
	}()

	for {
		select {
		case <-w.stopCh:
			return nil
		case err := <-srvErrCh:
			return err
		case <-w.wakeCh:
			if !w.RenderOnce(onFrame) {
				_ = w.Close()
				return nil
			}
		}
	}
}

// Handler returns the http.Handler serving:
//   - GET  /       HTML5 HiDPI <canvas> host page
//   - GET  /frame  Latest frame JSON snapshot
//   - GET  /events Server-Sent Events (SSE) live frame stream
//   - POST /event  Browser input event ingestion
func (w *WebDriver) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", w.handleIndex)
	mux.HandleFunc("/frame", w.handleFrame)
	mux.HandleFunc("/events", w.handleSSE)
	mux.HandleFunc("/event", w.handleEvent)
	return mux
}

func (w *WebDriver) handleIndex(rw http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(rw, r)
		return
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(rw, w.RenderHTML())
}

func (w *WebDriver) handleFrame(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.mu.Lock()
	data := w.lastJSON
	if len(data) == 0 {
		frame := WebFrame{
			Width:    w.width,
			Height:   w.height,
			Commands: []DrawCommand{},
		}
		data, _ = json.Marshal(frame)
	}
	w.mu.Unlock()

	rw.Header().Set("Content-Type", "application/json")
	rw.Header().Set("Cache-Control", "no-store")
	_, _ = rw.Write(data)
}

func (w *WebDriver) handleSSE(rw http.ResponseWriter, r *http.Request) {
	flusher, ok := rw.(http.Flusher)
	if !ok {
		http.Error(rw, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")

	ch := make(chan []byte, 8)
	w.subMu.Lock()
	w.subscribers[ch] = struct{}{}
	w.subMu.Unlock()
	defer func() {
		w.subMu.Lock()
		delete(w.subscribers, ch)
		w.subMu.Unlock()
	}()

	// Immediately send the current frame if available.
	w.mu.Lock()
	initial := w.lastJSON
	w.mu.Unlock()
	if len(initial) > 0 {
		_, _ = fmt.Fprintf(rw, "data: %s\n\n", initial)
		flusher.Flush()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-w.stopCh:
			return
		case payload := <-ch:
			_, _ = fmt.Fprintf(rw, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

type browserEventPayload struct {
	Type   string  `json:"type"`
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	Width  int     `json:"width"`
	Height int     `json:"height"`
	Key    string  `json:"key"`
	Char   string  `json:"char"`
	Shift  bool    `json:"shift"`
}

func (w *WebDriver) handleEvent(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload browserEventPayload
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&payload); err != nil {
		http.Error(rw, "invalid event JSON", http.StatusBadRequest)
		return
	}

	ev, ok := translateBrowserEvent(payload)
	if !ok {
		rw.WriteHeader(http.StatusNoContent)
		return
	}

	w.pushEvent(ev)
	rw.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(rw, `{"ok":true}`)
}

func translateBrowserEvent(p browserEventPayload) (Event, bool) {
	switch strings.ToLower(strings.TrimSpace(p.Type)) {
	case "click":
		return Event{Type: EventClick, X: p.X, Y: p.Y}, true
	case "dblclick", "doubleclick":
		return Event{Type: EventDoubleClick, X: p.X, Y: p.Y}, true
	case "mousedown":
		return Event{Type: EventMouseDown, X: p.X, Y: p.Y}, true
	case "mouseup":
		return Event{Type: EventMouseUp, X: p.X, Y: p.Y}, true
	case "mousemove":
		return Event{Type: EventMouseMove, X: p.X, Y: p.Y}, true
	case "resize":
		if p.Width <= 0 || p.Height <= 0 {
			return Event{}, false
		}
		return Event{Type: EventResize, Width: p.Width, Height: p.Height}, true
	case "keydown":
		key := normalizeBrowserKey(p.Key)
		if key == "" {
			return Event{}, false
		}
		return Event{Type: EventKeyDown, Key: key, Shift: p.Shift}, true
	case "keyup":
		key := normalizeBrowserKey(p.Key)
		if key == "" {
			return Event{}, false
		}
		return Event{Type: EventKeyUp, Key: key, Shift: p.Shift}, true
	case "char", "keypress":
		src := p.Char
		if src == "" {
			src = p.Key
		}
		runes := []rune(src)
		if len(runes) != 1 || runes[0] < 0x20 || runes[0] == 0x7F {
			return Event{}, false
		}
		return Event{Type: EventChar, Rune: runes[0]}, true
	default:
		return Event{}, false
	}
}

func normalizeBrowserKey(k string) string {
	switch k {
	case "Backspace":
		return KeyBackspace
	case "Enter":
		return KeyEnter
	case "Tab":
		return KeyTab
	case "Escape", "Esc":
		return KeyEscape
	case "ArrowLeft", "Left":
		return KeyLeft
	case "ArrowRight", "Right":
		return KeyRight
	case "ArrowUp", "Up":
		return KeyUp
	case "ArrowDown", "Down":
		return KeyDown
	case "Delete":
		return KeyDelete
	case "Home":
		return KeyHome
	case "End":
		return KeyEnd
	default:
		return ""
	}
}

func (w *WebDriver) broadcast(data []byte) {
	w.subMu.Lock()
	defer w.subMu.Unlock()
	for ch := range w.subscribers {
		select {
		case ch <- data:
		default:
			// Drop stale frame if slow client has a full buffer.
		}
	}
}

func colorToCSS(c Color) string {
	if c.A == 0xFF {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("rgba(%d,%d,%d,%.3f)", c.R, c.G, c.B, float64(c.A)/255.0)
}

// RenderHTML generates the responsive, SEO-friendly HTML5 shell with HiDPI
// <canvas> rendering and zero-dependency vanilla JS event/SSE bridge.
func (w *WebDriver) RenderHTML() string {
	w.mu.Lock()
	meta := w.meta
	w.mu.Unlock()

	title := html.EscapeString(meta.Title)
	if title == "" {
		title = "Gopkin App"
	}
	desc := html.EscapeString(meta.Description)
	if desc == "" {
		desc = "Built with Gopkin — Pure-Go Cross-Platform Framework"
	}
	appID := html.EscapeString(meta.AppID)
	version := html.EscapeString(meta.Version)

	var extraMeta strings.Builder
	if appID != "" {
		extraMeta.WriteString(fmt.Sprintf("  <meta name=\"application-id\" content=\"%s\">\n", appID))
	}
	if version != "" {
		extraMeta.WriteString(fmt.Sprintf("  <meta name=\"application-version\" content=\"%s\">\n", version))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>%s</title>
  <meta name="description" content="%s">
  <meta name="application-name" content="%s">
  <meta name="generator" content="Gopkin Pure-Go Web Target">
%s  <style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    html, body { width: 100%%; height: 100%%; overflow: hidden; background: #FFFFFF; color: #000000; font-family: -apple-system, BlinkMacSystemFont, "Inter", "Segoe UI", sans-serif; }
    #gopkin-canvas { display: block; width: 100vw; height: 100vh; outline: none; cursor: default; }
  </style>
</head>
<body>
  <canvas id="gopkin-canvas" tabindex="0" aria-label="%s"></canvas>
  <script>
  (function() {
    const canvas = document.getElementById("gopkin-canvas");
    const ctx = canvas.getContext("2d");
    let lastFrame = null;

    function syncCanvasSize() {
      const dpr = window.devicePixelRatio || 1;
      const w = Math.max(1, window.innerWidth);
      const h = Math.max(1, window.innerHeight);
      canvas.width = Math.round(w * dpr);
      canvas.height = Math.round(h * dpr);
      canvas.style.width = w + "px";
      canvas.style.height = h + "px";
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      return { width: w, height: h };
    }

    function drawRoundedRect(x, y, w, h, r) {
      const radius = Math.min(r || 0, w / 2, h / 2);
      ctx.beginPath();
      ctx.moveTo(x + radius, y);
      ctx.arcTo(x + w, y, x + w, y + h, radius);
      ctx.arcTo(x + w, y + h, x, y + h, radius);
      ctx.arcTo(x, y + h, x, y, radius);
      ctx.arcTo(x, y, x + w, y, radius);
      ctx.closePath();
      ctx.fill();
    }

    function renderFrame(frame) {
      if (!frame || !Array.isArray(frame.commands)) return;
      lastFrame = frame;
      const dpr = window.devicePixelRatio || 1;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, canvas.width / dpr, canvas.height / dpr);

      for (const cmd of frame.commands) {
        switch (cmd.op) {
          case "clear":
            ctx.fillStyle = cmd.color || "#ffffff";
            ctx.fillRect(0, 0, canvas.width / dpr, canvas.height / dpr);
            break;
          case "fillRect":
          case "drawRect":
            ctx.fillStyle = cmd.color || "#000000";
            ctx.fillRect(cmd.x || 0, cmd.y || 0, cmd.w || 0, cmd.h || 0);
            break;
          case "roundedRect":
            ctx.fillStyle = cmd.color || "#000000";
            drawRoundedRect(cmd.x || 0, cmd.y || 0, cmd.w || 0, cmd.h || 0, cmd.radius || 0);
            break;
          case "text":
            ctx.fillStyle = cmd.color || "#000000";
            ctx.font = (cmd.size || 16) + "px -apple-system, BlinkMacSystemFont, Inter, Segoe UI, sans-serif";
            ctx.textBaseline = "top";
            ctx.fillText(cmd.text || "", cmd.x || 0, cmd.y || 0);
            break;
          case "polyline":
            if (Array.isArray(cmd.points) && cmd.points.length > 0) {
              ctx.strokeStyle = cmd.color || "#000000";
              ctx.lineWidth = cmd.width || 1;
              ctx.beginPath();
              ctx.moveTo(cmd.points[0].X, cmd.points[0].Y);
              for (let i = 1; i < cmd.points.length; i++) {
                ctx.lineTo(cmd.points[i].X, cmd.points[i].Y);
              }
              ctx.stroke();
            }
            break;
        }
      }
    }

    function sendEvent(payload) {
      fetch("/event", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload)
      }).catch(function() {});
    }

    function pointerPos(e) {
      const rect = canvas.getBoundingClientRect();
      return { x: e.clientX - rect.left, y: e.clientY - rect.top };
    }

    canvas.addEventListener("mousedown", function(e) {
      canvas.focus();
      const p = pointerPos(e);
      sendEvent({ type: "mousedown", x: p.x, y: p.y });
    });
    canvas.addEventListener("mouseup", function(e) {
      const p = pointerPos(e);
      sendEvent({ type: "mouseup", x: p.x, y: p.y });
    });
    canvas.addEventListener("click", function(e) {
      const p = pointerPos(e);
      sendEvent({ type: "click", x: p.x, y: p.y });
    });
    canvas.addEventListener("dblclick", function(e) {
      const p = pointerPos(e);
      sendEvent({ type: "dblclick", x: p.x, y: p.y });
    });
    canvas.addEventListener("mousemove", function(e) {
      if (e.buttons !== 0) {
        const p = pointerPos(e);
        sendEvent({ type: "mousemove", x: p.x, y: p.y });
      }
    });

    window.addEventListener("keydown", function(e) {
      const special = ["Backspace", "Enter", "Tab", "Escape", "ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "Delete", "Home", "End"];
      if (special.includes(e.key)) {
        e.preventDefault();
        sendEvent({ type: "keydown", key: e.key, shift: !!e.shiftKey });
        return;
      }
      if (!e.ctrlKey && !e.metaKey && !e.altKey && e.key && e.key.length === 1) {
        sendEvent({ type: "char", char: e.key });
      }
    });

    window.addEventListener("resize", function() {
      const sz = syncCanvasSize();
      sendEvent({ type: "resize", width: sz.width, height: sz.height });
      if (lastFrame) renderFrame(lastFrame);
    });

    const initialSize = syncCanvasSize();
    fetch("/frame").then(function(r) { return r.json(); }).then(renderFrame).catch(function() {});

    if (typeof EventSource !== "undefined") {
      const es = new EventSource("/events");
      es.onmessage = function(ev) {
        try { renderFrame(JSON.parse(ev.data)); } catch (_) {}
      };
    }

    sendEvent({ type: "resize", width: initialSize.width, height: initialSize.height });
    canvas.focus();
  })();
  </script>
</body>
</html>
`, title, desc, title, extraMeta.String(), title)
}
