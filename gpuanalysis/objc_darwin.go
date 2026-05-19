//go:build darwin

package gpuanalysis

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// This file is the minimal Objective-C runtime + Metal entrypoint
// loader we use from pure Go via purego. It exposes:
//
//   - sel(name)      : interned selector for an ObjC method name
//   - cls(name)      : Objective-C class pointer
//   - msgN           : objc_msgSend variants (pointer / int / void
//                      returns and a couple of common argument
//                      shapes). Metal's API is reached entirely
//                      through these primitives.
//
// We intentionally do not generate a full ObjC bridge; we just hand-
// roll the calls the analyzer needs. That keeps the binding surface
// small enough to audit and avoids dragging in a heavyweight FFI
// generator just for a few dozen selectors.

var (
	libObjC   uintptr
	libMetal  uintptr
	libFound  sync.Once
	libErr    error
	loadMu    sync.Mutex
	selectors sync.Map // map[string]uintptr

	// Function pointers, resolved lazily once libraries are open.
	objcGetClass       func(name string) uintptr
	selRegisterName    func(name string) uintptr
	mtlCreateDefault   func() uintptr
	objcMsgSendPtr     uintptr // resolved once; called via purego.SyscallN
	objcAutoreleaseNew func() uintptr
	objcAutoreleaseDel func(pool uintptr)
)

func loadLibraries() error {
	libFound.Do(func() {
		o, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			libErr = fmt.Errorf("dlopen libobjc: %w", err)
			return
		}
		libObjC = o
		m, err := purego.Dlopen("/System/Library/Frameworks/Metal.framework/Metal", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			libErr = fmt.Errorf("dlopen Metal: %w", err)
			return
		}
		libMetal = m

		// Resolve the symbols we care about.
		purego.RegisterLibFunc(&objcGetClass, libObjC, "objc_getClass")
		purego.RegisterLibFunc(&selRegisterName, libObjC, "sel_registerName")
		purego.RegisterLibFunc(&mtlCreateDefault, libMetal, "MTLCreateSystemDefaultDevice")
		// Autorelease pool entry points are in libobjc.
		purego.RegisterLibFunc(&objcAutoreleaseNew, libObjC, "objc_autoreleasePoolPush")
		purego.RegisterLibFunc(&objcAutoreleaseDel, libObjC, "objc_autoreleasePoolPop")

		ptr, err := purego.Dlsym(libObjC, "objc_msgSend")
		if err != nil {
			libErr = fmt.Errorf("dlsym objc_msgSend: %w", err)
			return
		}
		objcMsgSendPtr = ptr
	})
	return libErr
}

// sel returns an interned Objective-C selector for the given name.
// Selectors are immutable and never freed; we cache them so repeated
// calls do not pay the sel_registerName cost per dispatch.
func sel(name string) uintptr {
	if v, ok := selectors.Load(name); ok {
		return v.(uintptr)
	}
	s := selRegisterName(name)
	selectors.Store(name, s)
	return s
}

// cls returns the class pointer for the given Objective-C class name.
// Classes are process-global; we look them up once per name.
func cls(name string) uintptr {
	return objcGetClass(name)
}

// msgSend invokes `objc_msgSend(receiver, selector, args...)` and
// returns the result as a uintptr. The variadic dispatch goes via
// purego.SyscallN. We do not try to vary the call signature based on
// argument shape; uintptr-sized arguments cover the Metal API surface
// we use because all of those calls take object pointers, NSInteger
// (which is intptr_t, i.e. int on 64-bit), or NSUInteger.
//
// Floating-point return / argument paths are deliberately not
// supported here because Metal compute does not need them; if a
// future kernel demands MTLSize-by-value we will add a typed wrapper
// for that specific case.
func msgSend(recv, selector uintptr, args ...uintptr) uintptr {
	full := make([]uintptr, 0, 2+len(args))
	full = append(full, recv, selector)
	full = append(full, args...)
	ret, _, _ := purego.SyscallN(objcMsgSendPtr, full...)
	return ret
}

// nsString allocates an NSString from a Go string. The returned
// pointer is autoreleased; the caller must hold an autorelease pool
// open across its lifetime, or retain it manually.
func nsString(s string) uintptr {
	cstr := append([]byte(s), 0)
	defer runtimeKeepalive(&cstr)
	clsNSString := cls("NSString")
	// +[NSString stringWithUTF8String:] takes a const char*.
	sel := sel("stringWithUTF8String:")
	return msgSend(clsNSString, sel, uintptr(unsafe.Pointer(&cstr[0])))
}

// runtimeKeepalive prevents the GC from collecting v while a pointer
// to it is in use from Objective-C/Metal land. It is a tiny wrapper
// over unsafe.Pointer that intentionally does not get inlined.
//
//go:noinline
func runtimeKeepalive(v any) { _ = v }

// withAutoreleasePool runs fn with a fresh Objective-C autorelease
// pool open. Metal API methods return autoreleased objects (for the
// "convenience constructors"); calling them outside a pool leaks the
// allocations. The wrapper is used by the Metal backend for per-frame
// command-buffer construction.
func withAutoreleasePool(fn func()) {
	pool := objcAutoreleaseNew()
	defer objcAutoreleaseDel(pool)
	fn()
}
