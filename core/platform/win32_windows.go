//go:build windows

package platform

import (
	"errors"
	"runtime"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// Win32 message and virtual-key constants.
const (
	wmDestroy     = 0x0002
	wmSize        = 0x0005
	wmClose       = 0x0010
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
	procFillRect         = user32.NewProc("FillRect")
	procGetKeyState      = user32.NewProc("GetKeyState")
	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procSetTextColor     = gdi32.NewProc("SetTextColor")
	procTextOutW         = gdi32.NewProc("TextOutW")
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

// Win32Driver is a zero-CGO Pure-Go Windows GUI Driver using user32/gdi32 syscalls.
type Win32Driver struct {
	mu            sync.Mutex
	hwnd          syscall.Handle
	hdc           syscall.Handle
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

// Init prepares the Win32 driver.
func (w *Win32Driver) Init() error {
	runtime.LockOSThread()
	return nil
}

// CreateWindow registers the Gopkin window class and creates a native Win32 window.
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

	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	return nil
}

// DrawRect fills a solid rectangle on the window's device context.
func (w *Win32Driver) DrawRect(x, y, width, height float32, color Color) {
	if w.hdc == 0 {
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
	procFillRect.Call(uintptr(w.hdc), uintptr(unsafe.Pointer(&r)), brush)
}

// DrawText renders UTF-16 text on the window's device context.
func (w *Win32Driver) DrawText(x, y float32, text string, _ float32, color Color) {
	if w.hdc == 0 || text == "" {
		return
	}
	u16 := utf16.Encode([]rune(text))
	if len(u16) == 0 {
		return
	}
	colorRef := uintptr(color.R) | uintptr(color.G)<<8 | uintptr(color.B)<<16
	procSetBkMode.Call(uintptr(w.hdc), transparentBkMode)
	procSetTextColor.Call(uintptr(w.hdc), colorRef)
	procTextOutW.Call(
		uintptr(w.hdc),
		uintptr(int32(x)),
		uintptr(int32(y)),
		uintptr(unsafe.Pointer(&u16[0])),
		uintptr(len(u16)),
	)
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

// RunLoop pumps Win32 messages and invokes onFrame until quit or onFrame returns false.
func (w *Win32Driver) RunLoop(onFrame func() bool) error {
	if w.hwnd == 0 {
		return errors.New("platform/win32: window not created")
	}

	var msg winMsg
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

		hdc, _, _ := procGetDC.Call(uintptr(w.hwnd))
		w.hdc = syscall.Handle(hdc)
		keepRunning := true
		if onFrame != nil {
			keepRunning = onFrame()
		}
		if hdc != 0 {
			procReleaseDC.Call(uintptr(w.hwnd), hdc)
			w.hdc = 0
		}

		w.mu.Lock()
		shouldQuit := w.quit
		w.mu.Unlock()
		if shouldQuit || !keepRunning {
			break
		}
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
			drv.pushEvent(Event{Type: EventResize, Width: width, Height: height})
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
			// Handle UTF-16 surrogate pairs for non-BMP Unicode characters.
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
			// Filter ASCII control characters (Backspace, Tab, Enter, Escape are handled via WM_KEYDOWN).
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
