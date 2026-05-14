# govpx Gio WebRTC remote demo

Native prototype for a TeamViewer-style remote desktop stack:

- Gio renders the viewer/control window.
- Pion WebRTC carries VP8 media plus a bidirectional input data channel.
- `-mode server` captures the host desktop, encodes it with govpx, and injects
  input received from the client.
- `-mode client` opens the Gio viewer, decodes the VP8 stream with govpx, and
  sends pointer/key input back to the server.

The demo uses a small HTTP endpoint for signalling. It no longer creates an
in-process host/viewer pair, so the viewer does not recursively capture itself
unless you deliberately run both modes on the same desktop.

This is a separate Go module so Gio and Pion stay out of the root `govpx`
module dependency graph.

## Run

On the machine being controlled:

```sh
cd examples/gio-webrtc-remote
go run . -mode server -addr :8090
```

On the viewer machine:

```sh
cd examples/gio-webrtc-remote
go run . -mode client -server http://SERVER_HOST:8090
```

Client mode is the default, so `go run . -server http://SERVER_HOST:8090` is
equivalent. Click inside the remote view to focus it; pointer movement, clicks,
scrolls, and keys are sent over the WebRTC data channel and acknowledged in the
footer. The native client header shows decoded FPS.

You can also use a browser client by opening:

```text
http://SERVER_HOST:8090/
```

The browser client receives the VP8 WebRTC video in a `<video>` element, shows a
decoded FPS counter, and sends pointer, wheel, and key input on the same
`input` data channel. The server receives those browser-originated events and
feeds them through the same host-side injection path as the native Gio client.

## What it proves

- A native Gio client can display VP8 frames decoded by govpx.
- A browser client can display the same VP8 WebRTC stream through the browser's
  native decoder.
- govpx can drive a WebRTC-style screen-content encode profile without libvpx.
- Pion carries the encoded frames as RTP/SRTP and input as SCTP data channel
  messages.
- Normal frames use the encoder's default per-frame reference behavior. The
  only explicit `EncodeFlags` bit used by the WebRTC path is
  `EncodeForceKeyFrame`, and only for startup or PLI/FIR RTCP feedback.
- The dependency graph is isolated to this example module.

## Real desktop capture

On macOS, the demo uses a real `desktopSource` backed by CoreGraphics:

- `CGDisplayCreateImage` captures the main display.
- Captured 32-bit display pixels are downscaled into an I420 `govpx.Image`.
- The I420 frame is passed directly to `govpx.EncodeInto`.

The implementation is in `platform_darwin.go` and uses `purego` dynamic calls
instead of cgo wrappers. macOS still requires Screen Recording permission; if
permission is missing the session fails with a clear error from startup.

Other platforms fall back to `syntheticDesktopSource` until a matching
`platform_$GOOS.go` file is added. The source boundary is:

```go
type desktopSource interface {
	Size() image.Point
	Capture(dst govpx.Image, frame int) error
}
```

For other operating systems, the capture options are separate from govpx:

- Windows: Desktop Duplication API or Windows Graphics Capture. Production use
  needs Win32/COM/WinRT calls; pure-Go syscall bindings are possible but
  platform-specific.
- Linux/X11: XShm/XGetImage-style capture. Pure-Go X protocol clients are
  possible, but high-performance capture usually goes through native X/SHM APIs.
- Linux/Wayland: use xdg-desktop-portal/PipeWire with user consent. This is the
  right security model, but it is not a simple standard-library API.

For real screen content, capture as BGRA/RGBA if that is what the OS gives you,
then convert into the `govpx.Image` I420 planes before encoding.

## Input outside the window

On macOS, the host side now consumes the WebRTC data-channel input messages and
posts them as absolute desktop events with CoreGraphics:

- mouse move/down/up/drag events use `CGEventCreateMouseEvent` and
  `CGEventPost`;
- keyboard events use `CGEventCreateKeyboardEvent`;
- coordinates are mapped from the scaled remote image back to the captured
  display bounds, so injected points can target the desktop outside the Gio
  window.

macOS requires Accessibility permission for this host-side injection. If the
client footer reports `macOS Accessibility permission is required`, grant
Accessibility access to the terminal/IDE/app that is running
`go run . -mode server`, then restart the server.

The viewer side still gets normal Gio pointer/key events for the remote image.
For a production viewer that captures local input even when the cursor leaves the
viewer window, add a platform event-tap/hook on the viewer side too:

- macOS: Accessibility/Input Monitoring permission; CGEvent taps and posting.
- Windows: low-level hooks for global capture, `SendInput` for injection.
- Linux/X11: XInput/XTest-style APIs.
- Linux/Wayland: compositor/portal-mediated remote-desktop APIs; arbitrary
  global capture/injection is intentionally restricted.

This demo implements the macOS host-side posting path now; it does not install a
global viewer-side event tap.

## C dependency note

The codec, capture, input, and WebRTC paths here avoid libvpx, FFmpeg, and cgo
wrappers. The macOS capture/input bridge uses `purego` to call system
frameworks dynamically. Gio's native desktop window backend is
platform-specific; on macOS and several Unix backends Gio itself uses cgo to
call operating-system graphics/window APIs. On this machine, normal
`go build ./...` succeeds, while:

```sh
CGO_ENABLED=0 go build ./...
```

fails inside Gio's GL backend. Treat this demo as "zero C codec/media
dependencies", not a universal `CGO_ENABLED=0` desktop app.
