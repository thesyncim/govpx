//go:build darwin

package main

import (
	"errors"
	"fmt"
	"image"
	"math"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/thesyncim/govpx"
)

const (
	coreGraphicsPath   = "/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics"
	coreFoundationPath = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"
	appServicesPath    = "/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices"

	cgEventTapHID = 0

	cgEventLeftMouseDown  = 1
	cgEventLeftMouseUp    = 2
	cgEventRightMouseDown = 3
	cgEventRightMouseUp   = 4
	cgEventMouseMoved     = 5
	cgEventLeftMouseDrag  = 6
	cgEventRightMouseDrag = 7
	cgEventScrollWheel    = 22
	cgEventOtherMouseDown = 25
	cgEventOtherMouseUp   = 26
	cgEventOtherMouseDrag = 27

	cgMouseButtonLeft   = 0
	cgMouseButtonRight  = 1
	cgMouseButtonCenter = 2

	cgScrollEventUnitPixel = 0

	cgMouseEventClickState   = 1
	cgMouseEventButtonNumber = 3

	cgImageAlphaInfoMask           = 0x1f
	cgImageAlphaPremultipliedLast  = 1
	cgImageAlphaPremultipliedFirst = 2
	cgImageAlphaLast               = 3
	cgImageAlphaFirst              = 4
	cgImageAlphaNoneSkipLast       = 5
	cgImageAlphaNoneSkipFirst      = 6

	cgBitmapByteOrderMask     = 0x7000
	cgBitmapByteOrder32Little = 0x2000
	cgBitmapByteOrder32Big    = 0x4000

	cgEventFlagMaskShift   = 1 << 17
	cgEventFlagMaskControl = 1 << 18
	cgEventFlagMaskAlt     = 1 << 19
	cgEventFlagMaskCommand = 1 << 20
)

type cgPoint struct {
	X float64
	Y float64
}

type cgSize struct {
	W float64
	H float64
}

type cgRect struct {
	Origin cgPoint
	Size   cgSize
}

var macCG struct {
	once sync.Once
	err  error

	cgMainDisplayID        func() uint32
	cgDisplayPixelsWide    func(uint32) uintptr
	cgDisplayPixelsHigh    func(uint32) uintptr
	cgDisplayBounds        func(uint32) cgRect
	cgDisplayCreateImage   func(uint32) uintptr
	cgImageGetWidth        func(uintptr) uintptr
	cgImageGetHeight       func(uintptr) uintptr
	cgImageGetBytesPerRow  func(uintptr) uintptr
	cgImageGetBitsPerPixel func(uintptr) uintptr
	cgImageGetBitmapInfo   func(uintptr) uint32
	cgImageGetDataProvider func(uintptr) uintptr
	cgDataProviderCopyData func(uintptr) uintptr

	cgPreflightScreenCaptureAccess func() bool
	cgRequestScreenCaptureAccess   func() bool

	cgEventCreateMouseEvent       func(uintptr, uint32, cgPoint, uint32) uintptr
	cgEventCreateKeyboardEvent    func(uintptr, uint16, bool) uintptr
	cgEventCreateScrollWheelEvent func(uintptr, uint32, uint32, int32, int32) uintptr
	cgEventSetFlags               func(uintptr, uint64)
	cgEventSetIntegerValueField   func(uintptr, uint32, int64)
	cgEventPost                   func(uint32, uintptr)

	cfDataGetBytePtr func(uintptr) uintptr
	cfDataGetLength  func(uintptr) int
	cfRelease        func(uintptr)

	axIsProcessTrusted func() bool
}

func loadMacCoreGraphics() error {
	macCG.once.Do(func() {
		cg, err := purego.Dlopen(coreGraphicsPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			macCG.err = err
			return
		}
		cf, err := purego.Dlopen(coreFoundationPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			macCG.err = err
			return
		}
		as, _ := purego.Dlopen(appServicesPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)

		purego.RegisterLibFunc(&macCG.cgMainDisplayID, cg, "CGMainDisplayID")
		purego.RegisterLibFunc(&macCG.cgDisplayPixelsWide, cg, "CGDisplayPixelsWide")
		purego.RegisterLibFunc(&macCG.cgDisplayPixelsHigh, cg, "CGDisplayPixelsHigh")
		purego.RegisterLibFunc(&macCG.cgDisplayBounds, cg, "CGDisplayBounds")
		purego.RegisterLibFunc(&macCG.cgDisplayCreateImage, cg, "CGDisplayCreateImage")
		purego.RegisterLibFunc(&macCG.cgImageGetWidth, cg, "CGImageGetWidth")
		purego.RegisterLibFunc(&macCG.cgImageGetHeight, cg, "CGImageGetHeight")
		purego.RegisterLibFunc(&macCG.cgImageGetBytesPerRow, cg, "CGImageGetBytesPerRow")
		purego.RegisterLibFunc(&macCG.cgImageGetBitsPerPixel, cg, "CGImageGetBitsPerPixel")
		purego.RegisterLibFunc(&macCG.cgImageGetBitmapInfo, cg, "CGImageGetBitmapInfo")
		purego.RegisterLibFunc(&macCG.cgImageGetDataProvider, cg, "CGImageGetDataProvider")
		purego.RegisterLibFunc(&macCG.cgDataProviderCopyData, cg, "CGDataProviderCopyData")

		registerOptionalCG(cg, "CGPreflightScreenCaptureAccess", &macCG.cgPreflightScreenCaptureAccess)
		registerOptionalCG(cg, "CGRequestScreenCaptureAccess", &macCG.cgRequestScreenCaptureAccess)

		purego.RegisterLibFunc(&macCG.cgEventCreateMouseEvent, cg, "CGEventCreateMouseEvent")
		purego.RegisterLibFunc(&macCG.cgEventCreateKeyboardEvent, cg, "CGEventCreateKeyboardEvent")
		purego.RegisterLibFunc(&macCG.cgEventSetFlags, cg, "CGEventSetFlags")
		registerOptionalCG(cg, "CGEventSetIntegerValueField", &macCG.cgEventSetIntegerValueField)
		purego.RegisterLibFunc(&macCG.cgEventPost, cg, "CGEventPost")
		registerOptionalCG(cg, "CGEventCreateScrollWheelEvent", &macCG.cgEventCreateScrollWheelEvent)

		purego.RegisterLibFunc(&macCG.cfDataGetBytePtr, cf, "CFDataGetBytePtr")
		purego.RegisterLibFunc(&macCG.cfDataGetLength, cf, "CFDataGetLength")
		purego.RegisterLibFunc(&macCG.cfRelease, cf, "CFRelease")
		if as != 0 {
			registerOptionalCG(as, "AXIsProcessTrusted", &macCG.axIsProcessTrusted)
		}
	})
	return macCG.err
}

func registerOptionalCG(handle uintptr, name string, fn any) {
	sym, err := purego.Dlsym(handle, name)
	if err != nil {
		return
	}
	purego.RegisterFunc(fn, sym)
}

func newDefaultDesktopSource() (desktopSource, error) {
	if err := loadMacCoreGraphics(); err != nil {
		return nil, err
	}
	if macCG.cgPreflightScreenCaptureAccess != nil && !macCG.cgPreflightScreenCaptureAccess() {
		if macCG.cgRequestScreenCaptureAccess != nil {
			macCG.cgRequestScreenCaptureAccess()
		}
		if !macCG.cgPreflightScreenCaptureAccess() {
			return nil, errors.New("macOS Screen Recording permission is required for display capture")
		}
	}

	display := macCG.cgMainDisplayID()
	pixels := image.Pt(int(macCG.cgDisplayPixelsWide(display)), int(macCG.cgDisplayPixelsHigh(display)))
	if pixels.X <= 0 || pixels.Y <= 0 {
		return nil, fmt.Errorf("invalid main display size %dx%d", pixels.X, pixels.Y)
	}
	bounds := macCG.cgDisplayBounds(display)
	if bounds.Size.W <= 0 || bounds.Size.H <= 0 {
		bounds = cgRect{Size: cgSize{W: float64(pixels.X), H: float64(pixels.Y)}}
	}

	return &macOSDesktopSource{
		displayID: display,
		pixels:    pixels,
		bounds:    bounds,
		out:       fitEven(pixels, image.Pt(desktopWidth, desktopHeight)),
	}, nil
}

type macOSDesktopSource struct {
	displayID uint32
	pixels    image.Point
	bounds    cgRect
	out       image.Point
}

func (s *macOSDesktopSource) Size() image.Point {
	return s.out
}

func (s *macOSDesktopSource) MapPoint(x, y int) (float64, float64) {
	x = clampInt(x, 0, s.out.X-1)
	y = clampInt(y, 0, s.out.Y-1)
	return s.bounds.Origin.X + float64(x)*s.bounds.Size.W/float64(s.out.X),
		s.bounds.Origin.Y + float64(y)*s.bounds.Size.H/float64(s.out.Y)
}

func (s *macOSDesktopSource) Capture(dst govpx.Image, frame int) error {
	if dst.Width != s.out.X || dst.Height != s.out.Y {
		return fmt.Errorf("macOS source needs %dx%d frame, got %dx%d", s.out.X, s.out.Y, dst.Width, dst.Height)
	}
	img := macCG.cgDisplayCreateImage(s.displayID)
	if img == 0 {
		return errors.New("CGDisplayCreateImage returned nil; grant Screen Recording permission and restart")
	}
	defer macCG.cfRelease(img)

	w := int(macCG.cgImageGetWidth(img))
	h := int(macCG.cgImageGetHeight(img))
	stride := int(macCG.cgImageGetBytesPerRow(img))
	bpp := int(macCG.cgImageGetBitsPerPixel(img))
	info := macCG.cgImageGetBitmapInfo(img)
	if w <= 0 || h <= 0 || stride <= 0 {
		return fmt.Errorf("invalid captured image geometry %dx%d stride=%d", w, h, stride)
	}
	if bpp != 32 {
		return fmt.Errorf("unsupported display pixel format: %d bits per pixel", bpp)
	}

	provider := macCG.cgImageGetDataProvider(img)
	if provider == 0 {
		return errors.New("CGImageGetDataProvider returned nil")
	}
	dataRef := macCG.cgDataProviderCopyData(provider)
	if dataRef == 0 {
		return errors.New("CGDataProviderCopyData returned nil")
	}
	defer macCG.cfRelease(dataRef)

	ptr := macCG.cfDataGetBytePtr(dataRef)
	n := macCG.cfDataGetLength(dataRef)
	if ptr == 0 || n <= 0 {
		return errors.New("empty captured display data")
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), n)
	if minLen := stride * h; len(data) < minLen {
		return fmt.Errorf("captured data too small: %d < %d", len(data), minLen)
	}

	bgraToI420(dst, data, w, h, stride, info)
	return nil
}

func newDefaultInputSink(source desktopSource) (inputSink, error) {
	if err := loadMacCoreGraphics(); err != nil {
		return nil, err
	}
	return macOSInputSink{source: source}, nil
}

type macOSInputSink struct {
	source desktopSource
}

func (s macOSInputSink) Handle(evt controlEvent) error {
	if !macAccessibilityTrusted() {
		return errors.New("macOS Accessibility permission is required for remote input injection; grant it to the app running the server and restart")
	}
	switch evt.Type {
	case "pointer":
		return s.handlePointer(evt)
	case "key":
		return s.handleKey(evt)
	default:
		return nil
	}
}

func (s macOSInputSink) Close() error { return nil }

func macAccessibilityTrusted() bool {
	return macCG.axIsProcessTrusted == nil || macCG.axIsProcessTrusted()
}

func (s macOSInputSink) handlePointer(evt controlEvent) error {
	x, y := mapHostPoint(s.source, evt.X, evt.Y)
	if evt.Kind == "Scroll" {
		return postMacScroll(evt.ScrollX, evt.ScrollY)
	}

	eventType, button := macMouseEvent(evt.Kind, evt.ButtonMask, evt.Buttons)
	event := macCG.cgEventCreateMouseEvent(0, eventType, cgPoint{X: x, Y: y}, button)
	if event == 0 {
		return errors.New("CGEventCreateMouseEvent returned nil")
	}
	defer macCG.cfRelease(event)
	if macCG.cgEventSetIntegerValueField != nil && (evt.Kind == "Press" || evt.Kind == "Release") {
		macCG.cgEventSetIntegerValueField(event, cgMouseEventClickState, 1)
		macCG.cgEventSetIntegerValueField(event, cgMouseEventButtonNumber, int64(button))
	}
	macCG.cgEventPost(cgEventTapHID, event)
	return nil
}

func (s macOSInputSink) handleKey(evt controlEvent) error {
	code, ok := macKeyCode(evt.Key)
	if !ok {
		return nil
	}
	down := evt.Kind == "Press"
	event := macCG.cgEventCreateKeyboardEvent(0, code, down)
	if event == 0 {
		return errors.New("CGEventCreateKeyboardEvent returned nil")
	}
	defer macCG.cfRelease(event)
	if flags := macModifierFlags(evt.Modifiers); flags != 0 {
		macCG.cgEventSetFlags(event, flags)
	}
	macCG.cgEventPost(cgEventTapHID, event)
	return nil
}

func mapHostPoint(source desktopSource, x, y int) (float64, float64) {
	if mapper, ok := source.(desktopPointMapper); ok {
		return mapper.MapPoint(x, y)
	}
	size := source.Size()
	return float64(clampInt(x, 0, size.X-1)), float64(clampInt(y, 0, size.Y-1))
}

func macMouseEvent(kind string, mask int, buttons string) (uint32, uint32) {
	button := macMouseButton(mask, buttons)
	switch kind {
	case "Press":
		switch button {
		case cgMouseButtonRight:
			return cgEventRightMouseDown, button
		case cgMouseButtonCenter:
			return cgEventOtherMouseDown, button
		default:
			return cgEventLeftMouseDown, button
		}
	case "Release":
		switch button {
		case cgMouseButtonRight:
			return cgEventRightMouseUp, button
		case cgMouseButtonCenter:
			return cgEventOtherMouseUp, button
		default:
			return cgEventLeftMouseUp, button
		}
	case "Drag":
		switch button {
		case cgMouseButtonRight:
			return cgEventRightMouseDrag, button
		case cgMouseButtonCenter:
			return cgEventOtherMouseDrag, button
		default:
			return cgEventLeftMouseDrag, button
		}
	default:
		return cgEventMouseMoved, cgMouseButtonLeft
	}
}

func macMouseButton(mask int, buttons string) uint32 {
	switch {
	case mask&2 != 0 || strings.Contains(buttons, "ButtonSecondary"):
		return cgMouseButtonRight
	case mask&4 != 0 || strings.Contains(buttons, "ButtonTertiary"):
		return cgMouseButtonCenter
	default:
		return cgMouseButtonLeft
	}
}

func postMacScroll(dx, dy int) error {
	if macCG.cgEventCreateScrollWheelEvent == nil {
		return nil
	}
	if dx == 0 && dy == 0 {
		return nil
	}
	event := macCG.cgEventCreateScrollWheelEvent(0, cgScrollEventUnitPixel, 2, int32(-dy), int32(dx))
	if event == 0 {
		return errors.New("CGEventCreateScrollWheelEvent returned nil")
	}
	defer macCG.cfRelease(event)
	macCG.cgEventPost(cgEventTapHID, event)
	return nil
}

func macModifierFlags(mods string) uint64 {
	var flags uint64
	if strings.Contains(mods, "Shift") {
		flags |= cgEventFlagMaskShift
	}
	if strings.Contains(mods, "Ctrl") {
		flags |= cgEventFlagMaskControl
	}
	if strings.Contains(mods, "Alt") {
		flags |= cgEventFlagMaskAlt
	}
	if strings.Contains(mods, "⌘") || strings.Contains(mods, "Command") {
		flags |= cgEventFlagMaskCommand
	}
	return flags
}

func macKeyCode(name string) (uint16, bool) {
	if len(name) == 1 {
		if code, ok := macLetterOrDigitKey[name]; ok {
			return code, true
		}
	}
	code, ok := macSpecialKeys[name]
	return code, ok
}

var macLetterOrDigitKey = map[string]uint16{
	"A": 0, "S": 1, "D": 2, "F": 3, "H": 4, "G": 5, "Z": 6, "X": 7, "C": 8, "V": 9,
	"B": 11, "Q": 12, "W": 13, "E": 14, "R": 15, "Y": 16, "T": 17, "1": 18, "2": 19,
	"3": 20, "4": 21, "6": 22, "5": 23, "=": 24, "9": 25, "7": 26, "-": 27, "8": 28,
	"0": 29, "]": 30, "O": 31, "U": 32, "[": 33, "I": 34, "P": 35, "L": 37, "J": 38,
	"'": 39, "K": 40, ";": 41, "\\": 42, ",": 43, "/": 44, "N": 45, "M": 46, ".": 47,
	"`": 50,
}

var macSpecialKeys = map[string]uint16{
	"Space": 49, "Tab": 48, "⏎": 36, "⌤": 76, "⎋": 53, "⌫": 51, "⌦": 117,
	"←": 123, "→": 124, "↓": 125, "↑": 126, "⇱": 115, "⇲": 119, "⇞": 116, "⇟": 121,
	"Enter": 36, "Return": 36, "Escape": 53, "Esc": 53, "Backspace": 51, "Delete": 117,
	"ArrowLeft": 123, "ArrowRight": 124, "ArrowDown": 125, "ArrowUp": 126,
	"Home": 115, "End": 119, "PageUp": 116, "PageDown": 121,
	"F1": 122, "F2": 120, "F3": 99, "F4": 118, "F5": 96, "F6": 97,
	"F7": 98, "F8": 100, "F9": 101, "F10": 109, "F11": 103, "F12": 111,
	"Shift": 56, "Ctrl": 59, "Control": 59, "Alt": 58, "⌘": 55, "Command": 55, "Meta": 55,
}

func fitEven(src image.Point, max image.Point) image.Point {
	scale := math.Min(float64(max.X)/float64(src.X), float64(max.Y)/float64(src.Y))
	if scale > 1 {
		scale = 1
	}
	w := int(float64(src.X) * scale)
	h := int(float64(src.Y) * scale)
	w &^= 1
	h &^= 1
	if w < 2 {
		w = 2
	}
	if h < 2 {
		h = 2
	}
	return image.Pt(w, h)
}

func bgraToI420(dst govpx.Image, data []byte, srcW, srcH, stride int, info uint32) {
	for y := 0; y < dst.Height; y++ {
		sy := y * srcH / dst.Height
		row := dst.Y[y*dst.YStride : y*dst.YStride+dst.Width]
		for x := 0; x < dst.Width; x++ {
			sx := x * srcW / dst.Width
			r, g, b := cgPixelRGB(data, sy*stride+sx*4, info)
			yy, _, _ := rgbToYUV(r, g, b)
			row[x] = yy
		}
	}

	uvW := (dst.Width + 1) / 2
	uvH := (dst.Height + 1) / 2
	for y := 0; y < uvH; y++ {
		uRow := dst.U[y*dst.UStride : y*dst.UStride+uvW]
		vRow := dst.V[y*dst.VStride : y*dst.VStride+uvW]
		for x := 0; x < uvW; x++ {
			var rSum, gSum, bSum, n int
			for dy := 0; dy < 2; dy++ {
				oy := min(y*2+dy, dst.Height-1)
				sy := oy * srcH / dst.Height
				for dx := 0; dx < 2; dx++ {
					ox := min(x*2+dx, dst.Width-1)
					sx := ox * srcW / dst.Width
					r, g, b := cgPixelRGB(data, sy*stride+sx*4, info)
					rSum += r
					gSum += g
					bSum += b
					n++
				}
			}
			_, uu, vv := rgbToYUV(rSum/n, gSum/n, bSum/n)
			uRow[x] = uu
			vRow[x] = vv
		}
	}
}

func cgPixelRGB(data []byte, off int, info uint32) (r, g, b int) {
	if off+3 >= len(data) {
		return 0, 0, 0
	}
	p := data[off : off+4]
	alpha := info & cgImageAlphaInfoMask
	switch info & cgBitmapByteOrderMask {
	case cgBitmapByteOrder32Big:
		switch alpha {
		case cgImageAlphaPremultipliedFirst, cgImageAlphaFirst, cgImageAlphaNoneSkipFirst:
			return int(p[1]), int(p[2]), int(p[3])
		default:
			return int(p[0]), int(p[1]), int(p[2])
		}
	case cgBitmapByteOrder32Little:
		switch alpha {
		case cgImageAlphaPremultipliedLast, cgImageAlphaLast, cgImageAlphaNoneSkipLast:
			return int(p[3]), int(p[2]), int(p[1])
		default:
			return int(p[2]), int(p[1]), int(p[0])
		}
	default:
		return int(p[2]), int(p[1]), int(p[0])
	}
}

func rgbToYUV(r, g, b int) (byte, byte, byte) {
	yy := ((66*r + 129*g + 25*b + 128) >> 8) + 16
	uu := ((-38*r - 74*g + 112*b + 128) >> 8) + 128
	vv := ((112*r - 94*g - 18*b + 128) >> 8) + 128
	return byte(clampInt(yy, 0, 255)), byte(clampInt(uu, 0, 255)), byte(clampInt(vv, 0, 255))
}
