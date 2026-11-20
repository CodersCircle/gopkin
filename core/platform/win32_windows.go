//go:build windows

package platform

import (
	"errors"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

// Win32 message, virtual-key, and GDI constants.
const (
	wmDestroy     = 0x0002
	wmSize        = 0x0005
	wmPaint       = 0x000F
	wmClose       = 0x0010
	wmEraseBkgnd  = 0x0014
	wmKeyDown     = 0x0100
	wmKeyUp       = 0x0101
	wmChar        = 0x0102
	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmLButtonDbl  = 0x0203

	vkBack   = 0x08
	vkTab    = 0x09
	vkReturn = 0x0D
	vkShift  = 0x10
	vkEscape = 0x1B
	vkEnd    = 0x23
	vkHome   = 0x24
	vkLeft   = 0x25
	vkUp     = 0x26
	vkRight  = 0x27
	vkDown   = 0x28
	vkDelete = 0x2E

	wsOverlappedWindow = 0x00CF0000
	wsVisible          = 0x10000000
	cwUseDefault       = 0x80000000
	swShow             = 5
	pmRemove           = 0x0001
	csHRedraw          = 0x0002
	csVRedraw          = 0x0001
	csDblClks          = 0x0008
	transparentBkMode  = 1
	srccopy            = 0x00CC0020
	psSolid            = 0
	psNull             = 5
	fwNormal           = 400
	fwSemiBold         = 600
	fwBold             = 700
	defaultCharset     = 1
	outDefaultPrecis   = 0
	clipDefaultPrecis  = 0
	cleartypeQuality   = 5
	defaultPitch       = 0
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	gdi32                = syscall.NewLazyDLL("gdi32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procPeekMessageW     = user32.NewProc("PeekMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procGetDC            = user32.NewProc("GetDC")
	procReleaseDC        = user32.NewProc("ReleaseDC")
	procBeginPaint       = user32.NewProc("BeginPaint")
	procEndPaint         = user32.NewProc("EndPaint")
	procGetClientRect    = user32.NewProc("GetClientRect")
	procFillRect         = user32.NewProc("FillRect")
	procGetKeyState      = user32.NewProc("GetKeyState")

	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	procCreatePen              = gdi32.NewProc("CreatePen")
	procRoundRect              = gdi32.NewProc("RoundRect")
	procMoveToEx               = gdi32.NewProc("MoveToEx")
	procLineTo                 = gdi32.NewProc("LineTo")
	procCreateFontW            = gdi32.NewProc("CreateFontW")
	procSetBkMode              = gdi32.NewProc("SetBkMode")
	procSetTextColor           = gdi32.NewProc("SetTextColor")
	procTextOutW               = gdi32.NewProc("TextOutW")
)

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       syscall.Handle
}

type winPoint struct {
	x, y int32
}

type winMsg struct {
	hwnd    syscall.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      winPoint
}

type winRect struct {
	left, top, right, bottom int32
}

type paintStruct struct {
	hdc         syscall.Handle
	fErase      int32
	rcPaint     winRect
	fRestore    int32
	fIncUpdate  int32
	rgbReserved [32]byte
}

// Win32Driver is a zero-CGO Pure-Go Windows GUI Driver using true offscreen
// GDI double buffering (memDC + memBmp + atomic BitBlt) for zero-flicker rendering.
type Win32Driver struct {
	mu            sync.Mutex
	hwnd          syscall.Handle
	memDC         syscall.Handle
	memBmp        syscall.Handle
	oldBmp        syscall.Handle
	bufW          int32
	bufH          int32
	events        []Event
	quit          bool
	highSurrogate rune
}

var (
	win32InstancesMu sync.RWMutex
	win32Instances   = make(map[syscall.Handle]*Win32Driver)
)

// NewWin32Driver creates a new Pure-Go Windows Driver.
func NewWin32Driver() *Win32Driver {
	return &Win32Driver{}
}

// Init prepares the Win32 driver on the calling OS thread.
func (w *Win32Driver) Init() error {
	runtime.LockOSThread()
	return nil
}

// CreateWindow registers the Gopkin window class (with HbrBackground = 0 to
// prevent flicker) and creates a native Win32 window.
func (w *Win32Driver) CreateWindow(title string, width, height int) error {
	hInst, _, _ := procGetModuleHandleW.Call(0)
	className, err := syscall.UTF16PtrFromString("GopkinWindowClass")
	if err != nil {
		return err
	}
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return err
	}

	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		style:         csHRedraw | csVRedraw | csDblClks,
		lpfnWndProc:   syscall.NewCallback(gopkinWndProc),
		hInstance:     syscall.Handle(hInst),
		hbrBackground: 0, // No automatic background brush — prevents erase flashes.
		lpszClassName: className,
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd, _, callErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(titlePtr)),
		wsOverlappedWindow|wsVisible,
		cwUseDefault, cwUseDefault,
		uintptr(width), uintptr(height),
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		return errors.New("platform/win32: CreateWindowExW failed: " + callErr.Error())
	}

	w.hwnd = syscall.Handle(hwnd)
	win32InstancesMu.Lock()
	win32Instances[w.hwnd] = w
	win32InstancesMu.Unlock()

	w.ensureBackBuffer(int32(width), int32(height))

	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	return nil
}

// ensureBackBuffer creates or resizes the persistent offscreen memDC and memBmp
// strictly when the window dimensions change, releasing previous GDI handles.
func (w *Win32Driver) ensureBackBuffer(width, height int32) {
	if width <= 0 || height <= 0 || w.hwnd == 0 {
		return
	}
	if w.memDC != 0 && w.memBmp != 0 && w.bufW == width && w.bufH == height {
		return
	}

	w.destroyBackBuffer()

	winDC, _, _ := procGetDC.Call(uintptr(w.hwnd))
	if winDC == 0 {
		return
	}
	defer procReleaseDC.Call(uintptr(w.hwnd), winDC)

	memDC, _, _ := procCreateCompatibleDC.Call(winDC)
	if memDC == 0 {
		return
	}
	memBmp, _, _ := procCreateCompatibleBitmap.Call(winDC, uintptr(width), uintptr(height))
	if memBmp == 0 {
		procDeleteDC.Call(memDC)
		return
	}
	oldBmp, _, _ := procSelectObject.Call(memDC, memBmp)

	w.memDC = syscall.Handle(memDC)
	w.memBmp = syscall.Handle(memBmp)
	w.oldBmp = syscall.Handle(oldBmp)
	w.bufW = width
	w.bufH = height
}

func (w *Win32Driver) destroyBackBuffer() {
	if w.memDC != 0 {
		if w.oldBmp != 0 {
			procSelectObject.Call(uintptr(w.memDC), uintptr(w.oldBmp))
			w.oldBmp = 0
		}
		if w.memBmp != 0 {
			procDeleteObject.Call(uintptr(w.memBmp))
			w.memBmp = 0
		}
		procDeleteDC.Call(uintptr(w.memDC))
		w.memDC = 0
	}
	w.bufW = 0
	w.bufH = 0
}

// Close releases all offscreen GDI resources cleanly.
func (w *Win32Driver) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.destroyBackBuffer()
	if w.hwnd != 0 {
		win32InstancesMu.Lock()
		delete(win32Instances, w.hwnd)
		win32InstancesMu.Unlock()
		w.hwnd = 0
	}
	return nil
}

// Clear fills the entire offscreen buffer with color.
func (w *Win32Driver) Clear(color Color) {
	if w.memDC == 0 {
		return
	}
	w.FillRect(0, 0, float32(w.bufW), float32(w.bufH), color)
}

// FillRect draws a solid filled rectangle strictly onto the offscreen memDC.
func (w *Win32Driver) FillRect(x, y, width, height float32, color Color) {
	if w.memDC == 0 || width <= 0 || height <= 0 {
		return
	}
	colorRef := uintptr(color.R) | uintptr(color.G)<<8 | uintptr(color.B)<<16
	brush, _, _ := procCreateSolidBrush.Call(colorRef)
	if brush == 0 {
		return
	}
	defer procDeleteObject.Call(brush)

	r := winRect{
		left:   int32(x),
		top:    int32(y),
		right:  int32(x + width),
		bottom: int32(y + height),
	}
	procFillRect.Call(uintptr(w.memDC), uintptr(unsafe.Pointer(&r)), brush)
}

// DrawRect implements Driver.DrawRect by drawing onto the offscreen memDC.
func (w *Win32Driver) DrawRect(x, y, width, height float32, color Color) {
	w.FillRect(x, y, width, height, color)
}

// DrawRoundedRect draws a solid filled rounded rectangle strictly onto memDC.
func (w *Win32Driver) DrawRoundedRect(x, y, width, height, radius float32, color Color) {
	if w.memDC == 0 || width <= 0 || height <= 0 {
		return
	}
	colorRef := uintptr(color.R) | uintptr(color.G)<<8 | uintptr(color.B)<<16
	brush, _, _ := procCreateSolidBrush.Call(colorRef)
	if brush == 0 {
		return
	}
	defer procDeleteObject.Call(brush)

	nullPen, _, _ := procCreatePen.Call(psNull, 0, 0)
	defer procDeleteObject.Call(nullPen)

	oldBrush, _, _ := procSelectObject.Call(uintptr(w.memDC), brush)
	oldPen, _, _ := procSelectObject.Call(uintptr(w.memDC), nullPen)

	diam := int32(radius * 2)
	procRoundRect.Call(
		uintptr(w.memDC),
		uintptr(int32(x)),
		uintptr(int32(y)),
		uintptr(int32(x+width+1)),
		uintptr(int32(y+height+1)),
		uintptr(diam),
		uintptr(diam),
	)

	procSelectObject.Call(uintptr(w.memDC), oldBrush)
	procSelectObject.Call(uintptr(w.memDC), oldPen)
}

// DrawPolyline strokes a multi-segment polyline strictly onto memDC.
func (w *Win32Driver) DrawPolyline(points []Point, lineWidth float32, color Color) {
	if w.memDC == 0 || len(points) < 2 {
		return
	}
	lw := int32(lineWidth)
	if lw < 1 {
		lw = 1
	}
	colorRef := uintptr(color.R) | uintptr(color.G)<<8 | uintptr(color.B)<<16
	pen, _, _ := procCreatePen.Call(psSolid, uintptr(lw), colorRef)
	if pen == 0 {
		return
	}
	defer procDeleteObject.Call(pen)

	oldPen, _, _ := procSelectObject.Call(uintptr(w.memDC), pen)
	procMoveToEx.Call(uintptr(w.memDC), uintptr(int32(points[0].X)), uintptr(int32(points[0].Y)), 0)
	for i := 1; i < len(points); i++ {
		procLineTo.Call(uintptr(w.memDC), uintptr(int32(points[i].X)), uintptr(int32(points[i].Y)))
	}
	procSelectObject.Call(uintptr(w.memDC), oldPen)
}

// DrawText renders ClearType UTF-16 text strictly onto memDC using Segoe UI scaled to size.
func (w *Win32Driver) DrawText(x, y float32, text string, size float32, color Color) {
	if w.memDC == 0 || text == "" {
		return
	}
	u16 := utf16.Encode([]rune(text))
	if len(u16) == 0 {
		return
	}

	if size <= 0 {
		size = 14
	}
	weight := uintptr(fwNormal)
	if size >= 22 {
		weight = fwBold
	} else if size >= 15 {
		weight = fwSemiBold
	}

	face, _ := syscall.UTF16PtrFromString("Segoe UI")
	fontHeight := -int32(size * 1.15)
	font, _, _ := procCreateFontW.Call(
		uintptr(fontHeight), // cHeight
		0,                   // cWidth
		0, 0,                // escapement, orientation
		weight,
		0, 0, 0, // italic, underline, strikeout
		defaultCharset,
		outDefaultPrecis,
		clipDefaultPrecis,
		cleartypeQuality,
		defaultPitch,
		uintptr(unsafe.Pointer(face)),
	)

	var oldFont uintptr
	if font != 0 {
		oldFont, _, _ = procSelectObject.Call(uintptr(w.memDC), font)
		defer func() {
			procSelectObject.Call(uintptr(w.memDC), oldFont)
			procDeleteObject.Call(font)
		}()
	}

	colorRef := uintptr(color.R) | uintptr(color.G)<<8 | uintptr(color.B)<<16
	procSetBkMode.Call(uintptr(w.memDC), transparentBkMode)
	procSetTextColor.Call(uintptr(w.memDC), colorRef)
	procTextOutW.Call(
		uintptr(w.memDC),
		uintptr(int32(x)),
		uintptr(int32(y)),
		uintptr(unsafe.Pointer(&u16[0])),
		uintptr(len(u16)),
	)
}

// Present atomically blits the completed offscreen memDC frame onto the live window DC.
func (w *Win32Driver) Present() {
	if w.hwnd == 0 || w.memDC == 0 || w.bufW <= 0 || w.bufH <= 0 {
		return
	}
	hdc, _, _ := procGetDC.Call(uintptr(w.hwnd))
	if hdc == 0 {
		return
	}
	procBitBlt.Call(
		hdc, 0, 0, uintptr(w.bufW), uintptr(w.bufH),
		uintptr(w.memDC), 0, 0, srccopy,
	)
	procReleaseDC.Call(uintptr(w.hwnd), hdc)
}

// PollEvents returns and clears all queued Win32 input and window events.
func (w *Win32Driver) PollEvents() []Event {
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

// RunLoop pumps Win32 messages, renders each frame into memDC, and presents via
// atomic BitBlt until the window closes or onFrame returns false.
func (w *Win32Driver) RunLoop(onFrame func() bool) error {
	if w.hwnd == 0 {
		return errors.New("platform/win32: window not created")
	}
	defer w.Close()

	var msg winMsg
	var rc winRect
	for {
		for {
			ret, _, _ := procPeekMessageW.Call(
				uintptr(unsafe.Pointer(&msg)),
				0, 0, 0, pmRemove,
			)
			if ret == 0 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		}

		// Keep back buffer synchronized with actual client area dimensions.
		if ret, _, _ := procGetClientRect.Call(uintptr(w.hwnd), uintptr(unsafe.Pointer(&rc))); ret != 0 {
			cw := rc.right - rc.left
			ch := rc.bottom - rc.top
			if cw > 0 && ch > 0 {
				w.ensureBackBuffer(cw, ch)
			}
		}

		keepRunning := true
		if onFrame != nil {
			keepRunning = onFrame()
		}
		w.Present()

		w.mu.Lock()
		shouldQuit := w.quit
		w.mu.Unlock()
		if shouldQuit || !keepRunning {
			break
		}
		time.Sleep(16 * time.Millisecond)
	}
	return nil
}

func (w *Win32Driver) pushEvent(ev Event) {
	w.mu.Lock()
	w.events = append(w.events, ev)
	w.mu.Unlock()
}

func isShiftDown() bool {
	state, _, _ := procGetKeyState.Call(vkShift)
	return (uint16(state) & 0x8000) != 0
}

func mapWin32Key(vk uintptr) string {
	switch vk {
	case vkBack:
		return KeyBackspace
	case vkTab:
		return KeyTab
	case vkReturn:
		return KeyEnter
	case vkEscape:
		return KeyEscape
	case vkLeft:
		return KeyLeft
	case vkRight:
		return KeyRight
	case vkUp:
		return KeyUp
	case vkDown:
		return KeyDown
	case vkDelete:
		return KeyDelete
	case vkHome:
		return KeyHome
	case vkEnd:
		return KeyEnd
	default:
		return ""
	}
}

func gopkinWndProc(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	win32InstancesMu.RLock()
	drv := win32Instances[hwnd]
	win32InstancesMu.RUnlock()

	if drv != nil {
		switch msg {
		case wmEraseBkgnd:
			// Return 1 immediately so Windows never flashes/erases background between frames.
			return 1

		case wmPaint:
			var ps paintStruct
			hdc, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
			if hdc != 0 && drv.memDC != 0 && drv.bufW > 0 && drv.bufH > 0 {
				procBitBlt.Call(
					hdc, 0, 0, uintptr(drv.bufW), uintptr(drv.bufH),
					uintptr(drv.memDC), 0, 0, srccopy,
				)
			}
			procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
			return 0

		case wmClose, wmDestroy:
			drv.mu.Lock()
			drv.quit = true
			drv.events = append(drv.events, Event{Type: EventQuit})
			drv.mu.Unlock()
			procPostQuitMessage.Call(0)
			return 0

		case wmSize:
			width := int(lParam & 0xFFFF)
			height := int((lParam >> 16) & 0xFFFF)
			if width > 0 && height > 0 {
				drv.ensureBackBuffer(int32(width), int32(height))
				drv.pushEvent(Event{Type: EventResize, Width: width, Height: height})
			}
			return 0

		case wmLButtonDown:
			x := float32(int16(lParam & 0xFFFF))
			y := float32(int16((lParam >> 16) & 0xFFFF))
			drv.pushEvent(Event{Type: EventMouseDown, X: x, Y: y})
			return 0

		case wmLButtonUp:
			x := float32(int16(lParam & 0xFFFF))
			y := float32(int16((lParam >> 16) & 0xFFFF))
			drv.pushEvent(Event{Type: EventMouseUp, X: x, Y: y})
			drv.pushEvent(Event{Type: EventClick, X: x, Y: y})
			return 0

		case wmLButtonDbl:
			x := float32(int16(lParam & 0xFFFF))
			y := float32(int16((lParam >> 16) & 0xFFFF))
			drv.pushEvent(Event{Type: EventDoubleClick, X: x, Y: y})
			return 0

		case wmMouseMove:
			x := float32(int16(lParam & 0xFFFF))
			y := float32(int16((lParam >> 16) & 0xFFFF))
			drv.pushEvent(Event{Type: EventMouseMove, X: x, Y: y})
			return 0

		case wmKeyDown:
			if key := mapWin32Key(wParam); key != "" {
				drv.pushEvent(Event{
					Type:  EventKeyDown,
					Key:   key,
					Shift: isShiftDown(),
				})
			}
			return 0

		case wmKeyUp:
			if key := mapWin32Key(wParam); key != "" {
				drv.pushEvent(Event{
					Type:  EventKeyUp,
					Key:   key,
					Shift: isShiftDown(),
				})
			}
			return 0

		case wmChar:
			r := rune(wParam)
			if r >= 0xD800 && r <= 0xDBFF {
				drv.highSurrogate = r
				return 0
			}
			if r >= 0xDC00 && r <= 0xDFFF && drv.highSurrogate != 0 {
				r = utf16.DecodeRune(drv.highSurrogate, r)
				drv.highSurrogate = 0
			} else {
				drv.highSurrogate = 0
			}
			if r >= 0x20 && r != 0x7F {
				drv.pushEvent(Event{
					Type: EventChar,
					Rune: r,
				})
			}
			return 0
		}
	}

	ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return ret
}
