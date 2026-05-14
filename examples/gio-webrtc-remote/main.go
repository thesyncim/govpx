// gio-webrtc-remote is a native Gio prototype for a TeamViewer-style remote
// desktop pipeline. Run it as a server on the machine being controlled and as
// a client on the viewer machine.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/pion/rtcp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"

	"github.com/thesyncim/govpx"
)

const (
	desktopWidth  = 1280
	desktopHeight = 720
	framerate     = 30
	clockRate     = 90000
	targetKbps    = 3200
)

const browserHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>govpx remote</title>
<style>
html,body{margin:0;height:100%;background:#121418;color:#e7ecf2;font-family:system-ui,-apple-system,Segoe UI,sans-serif}
body{display:flex;flex-direction:column}
header{display:flex;align-items:center;gap:20px;padding:12px 16px;background:#151a20;border-bottom:1px solid #2f3842}
h1{font-size:18px;line-height:1;margin:0;font-weight:650}
#stats{color:#aab6c2;font-size:14px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
main{flex:1;min-height:0;display:flex;background:#080a0c}
video{width:100%;height:100%;object-fit:contain;background:#000;outline:none;cursor:crosshair}
footer{display:flex;gap:16px;padding:10px 16px;background:#151a20;border-top:1px solid #2f3842;color:#7eace0;font-size:14px}
#status{flex:1;color:#bcc6d1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
#input{flex:1;text-align:right;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
</style>
</head>
<body>
<header><h1>govpx remote</h1><div id="stats">connecting...</div></header>
<main><video id="remote" autoplay playsinline muted tabindex="0"></video></main>
<footer><div id="status">starting WebRTC</div><div id="input">click the video to send input</div></footer>
<script>
const video = document.getElementById("remote");
const stats = document.getElementById("stats");
const status = document.getElementById("status");
const input = document.getElementById("input");
const pc = new RTCPeerConnection();
const dc = pc.createDataChannel("input");
let lastButtonMask = 0;
let presented = 0;
let lastPresented = 0;
let lastFPSAt = performance.now();

function setStatus(text){ status.textContent = text; }
function setInput(text){ input.textContent = text; }
function send(payload){
  if (dc.readyState !== "open") return;
  dc.send(JSON.stringify(payload));
}
function buttonNames(mask){
  const out = [];
  if (mask & 1) out.push("ButtonPrimary");
  if (mask & 2) out.push("ButtonSecondary");
  if (mask & 4) out.push("ButtonTertiary");
  return out.join("|");
}
function buttonMaskFromButton(button){
  if (button === 2) return 2;
  if (button === 1) return 4;
  return 1;
}
function videoPoint(e, clamp){
  const rect = video.getBoundingClientRect();
  const vw = video.videoWidth || 960;
  const vh = video.videoHeight || 540;
  const scale = Math.min(rect.width / vw, rect.height / vh);
  const contentW = vw * scale;
  const contentH = vh * scale;
  const ox = (rect.width - contentW) / 2;
  const oy = (rect.height - contentH) / 2;
  let x = (e.clientX - rect.left - ox) / scale;
  let y = (e.clientY - rect.top - oy) / scale;
  if (!clamp && (x < 0 || y < 0 || x >= vw || y >= vh)) return null;
  x = Math.max(0, Math.min(vw - 1, Math.round(x)));
  y = Math.max(0, Math.min(vh - 1, Math.round(y)));
  return {x, y};
}
function pointerPayload(e, kind, clamp){
  const p = videoPoint(e, clamp);
  if (!p) return null;
  let mask = e.buttons || 0;
  if (kind === "Press" && mask === 0) mask = buttonMaskFromButton(e.button);
  if (kind === "Release") mask = buttonMaskFromButton(e.button);
  if (mask !== 0) lastButtonMask = mask;
  if (kind === "Release" || kind === "Cancel") lastButtonMask = e.buttons || 0;
  return {
    type: "pointer",
    kind,
    x: p.x,
    y: p.y,
    buttons: buttonNames(mask),
    button_mask: mask
  };
}
function sendPointer(e, kind, clamp){
  const payload = pointerPayload(e, kind, clamp);
  if (!payload) return;
  e.preventDefault();
  video.focus();
  send(payload);
  setInput(kind + " x=" + payload.x + " y=" + payload.y);
}
function keyName(e){
  if (e.key === " ") return "Space";
  if (e.key === "Meta") return "Command";
  if (e.key === "Control") return "Ctrl";
  if (e.key.length === 1) return e.key.toUpperCase();
  return e.key;
}
function modifiers(e){
  const out = [];
  if (e.shiftKey) out.push("Shift");
  if (e.ctrlKey) out.push("Ctrl");
  if (e.altKey) out.push("Alt");
  if (e.metaKey) out.push("Command");
  return out.join("|");
}
function sendKey(e, kind){
  send({type:"key",kind,key:keyName(e),modifiers:modifiers(e)});
  setInput(kind + " key=" + keyName(e));
  e.preventDefault();
}

dc.onopen = () => setStatus("input channel open");
dc.onclose = () => setStatus("input channel closed");
dc.onmessage = e => setInput("server: " + e.data);
pc.addTransceiver("video", {direction:"recvonly"});
pc.ontrack = e => { video.srcObject = e.streams[0]; };
pc.onconnectionstatechange = () => setStatus("peer: " + pc.connectionState);
pc.oniceconnectionstatechange = () => setStatus("ICE: " + pc.iceConnectionState);

video.addEventListener("pointerdown", e => {
  video.setPointerCapture(e.pointerId);
  sendPointer(e, "Press", false);
});
video.addEventListener("pointerup", e => sendPointer(e, "Release", true));
video.addEventListener("pointercancel", e => sendPointer(e, "Cancel", true));
video.addEventListener("pointermove", e => sendPointer(e, e.buttons ? "Drag" : "Move", !!e.buttons));
video.addEventListener("wheel", e => {
  const p = videoPoint(e, false);
  if (!p) return;
  e.preventDefault();
  video.focus();
  send({type:"pointer",kind:"Scroll",x:p.x,y:p.y,scroll_x:Math.round(e.deltaX),scroll_y:Math.round(e.deltaY)});
  setInput("Scroll x=" + p.x + " y=" + p.y);
}, {passive:false});
video.addEventListener("keydown", e => sendKey(e, "Press"));
video.addEventListener("keyup", e => sendKey(e, "Release"));
video.addEventListener("contextmenu", e => e.preventDefault());

function updateFPS(now, frameCount){
  presented = frameCount;
  if (now - lastFPSAt >= 1000) {
    const fps = (presented - lastPresented) * 1000 / (now - lastFPSAt);
    stats.textContent = (video.videoWidth || 0) + "x" + (video.videoHeight || 0) + " VP8 | " + fps.toFixed(1) + " fps";
    lastPresented = presented;
    lastFPSAt = now;
  }
}
if ("requestVideoFrameCallback" in HTMLVideoElement.prototype) {
  const onFrame = (now, metadata) => {
    updateFPS(now, metadata.presentedFrames || presented + 1);
    video.requestVideoFrameCallback(onFrame);
  };
  video.requestVideoFrameCallback(onFrame);
} else {
  setInterval(() => updateFPS(performance.now(), video.webkitDecodedFrameCount || presented), 1000);
}

async function start(){
  await pc.setLocalDescription(await pc.createOffer());
  await new Promise(resolve => {
    if (pc.iceGatheringState === "complete") return resolve();
    pc.onicegatheringstatechange = () => pc.iceGatheringState === "complete" && resolve();
  });
  const res = await fetch("/offer", {
    method: "POST",
    headers: {"Content-Type":"application/json"},
    body: JSON.stringify(pc.localDescription)
  });
  if (!res.ok) throw new Error(await res.text());
  await pc.setRemoteDescription(await res.json());
}
start().catch(e => setStatus("error: " + e.message));
</script>
</body>
</html>`

func main() {
	mode := flag.String("mode", "client", "mode: server or client")
	addr := flag.String("addr", ":8090", "server listen address")
	serverURL := flag.String("server", "http://localhost:8090", "server URL used by client mode")
	flag.Parse()

	switch *mode {
	case "server":
		if err := runServer(*addr); err != nil {
			log.Fatal(err)
		}
		return
	case "client":
	default:
		log.Fatalf("unknown -mode %q; use server or client", *mode)
	}

	go func() {
		w := new(app.Window)
		w.Option(
			app.Title("govpx Gio WebRTC Remote"),
			app.Size(unit.Dp(1120), unit.Dp(760)),
			app.MinSize(unit.Dp(760), unit.Dp(520)),
		)
		if err := newUI(w, *serverURL).run(); err != nil {
			log.Fatal(err)
		}
	}()
	app.Main()
}

type uiState struct {
	w  *app.Window
	th *material.Theme

	ops            op.Ops
	start          widget.Clickable
	stop           widget.Clickable
	remoteTag      struct{}
	autoStart      bool
	serverURL      string
	display        image.Point
	imageSize      image.Point
	lastButtonMask int
	renderedID     uint64
	imageOp        paint.ImageOp

	mu       sync.RWMutex
	starting bool
	session  *clientSession
	status   string
	control  string
	frame    *image.NRGBA
	frameID  uint64
}

type snapshot struct {
	starting bool
	running  bool
	status   string
	control  string
	frame    *image.NRGBA
	frameID  uint64

	decoded  uint64
	fps      float64
	sent     uint64
	received uint64
}

func newUI(w *app.Window, serverURL string) *uiState {
	th := material.NewTheme()
	th.Palette = material.Palette{
		Bg:         color.NRGBA{R: 21, G: 24, B: 28, A: 255},
		Fg:         color.NRGBA{R: 231, G: 236, B: 242, A: 255},
		ContrastBg: color.NRGBA{R: 40, G: 114, B: 190, A: 255},
		ContrastFg: color.NRGBA{R: 255, G: 255, B: 255, A: 255},
	}
	return &uiState{
		w:         w,
		th:        th,
		serverURL: serverURL,
		status:    "ready",
	}
}

func (ui *uiState) run() error {
	for {
		switch e := ui.w.Event().(type) {
		case app.DestroyEvent:
			ui.stopSession()
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ui.ops, e)
			ui.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (ui *uiState) layout(gtx layout.Context) layout.Dimensions {
	if !ui.autoStart {
		ui.autoStart = true
		ui.startSession()
	}
	for ui.start.Clicked(gtx) {
		ui.startSession()
	}
	for ui.stop.Clicked(gtx) {
		ui.stopSession()
	}

	snap := ui.snapshot()
	paint.FillShape(gtx.Ops, color.NRGBA{R: 18, G: 20, B: 24, A: 255}, clip.Rect{Max: gtx.Constraints.Max}.Op())

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.layoutHeader(gtx, snap)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return ui.layoutRemote(gtx, snap)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.layoutFooter(gtx, snap)
		}),
	)
}

func (ui *uiState) layoutHeader(gtx layout.Context, snap snapshot) layout.Dimensions {
	return layout.Inset{Top: 12, Bottom: 10, Left: 16, Right: 16}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		title := "govpx remote"
		state := "starting"
		switch {
		case snap.running:
			state = "connected"
		case !snap.starting:
			state = "stopped"
		}
		metrics := fmt.Sprintf("%s  |  %dx%d VP8  |  %.1f fps  |  decoded %d  input sent %d  ack %d",
			state, desktopWidth, desktopHeight, snap.fps, snap.decoded, snap.sent, snap.received)

		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.H6(ui.th, title)
				l.Color = color.NRGBA{R: 245, G: 248, B: 252, A: 255}
				return l.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Width: 18}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(ui.th, metrics)
				l.Color = color.NRGBA{R: 170, G: 182, B: 194, A: 255}
				l.MaxLines = 1
				return l.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				b := material.Button(ui.th, &ui.start, "Reconnect")
				b.Background = color.NRGBA{R: 42, G: 122, B: 201, A: 255}
				b.CornerRadius = 6
				return b.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				b := material.Button(ui.th, &ui.stop, "Stop")
				b.Background = color.NRGBA{R: 58, G: 64, B: 72, A: 255}
				b.CornerRadius = 6
				return b.Layout(gtx)
			}),
		)
	})
}

func (ui *uiState) layoutRemote(gtx layout.Context, snap snapshot) layout.Dimensions {
	ui.handleRemoteInput(gtx)

	size := gtx.Constraints.Max
	paint.FillShape(gtx.Ops, color.NRGBA{R: 8, G: 10, B: 12, A: 255}, clip.Rect{Max: size}.Op())

	if snap.frame == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Body1(ui.th, "waiting for remote video")
			l.Color = color.NRGBA{R: 134, G: 146, B: 160, A: 255}
			return l.Layout(gtx)
		})
	}

	frameSize := snap.frame.Bounds().Size()
	display := contain(size, frameSize)
	ui.display = display.Size()
	ui.imageSize = frameSize

	paint.FillShape(gtx.Ops, color.NRGBA{R: 0, G: 0, B: 0, A: 255}, clip.Rect(display).Op())
	stack := op.Offset(display.Min).Push(gtx.Ops)
	cgtx := gtx
	cgtx.Constraints = layout.Exact(display.Size())
	if snap.frameID != ui.renderedID {
		ui.imageOp = paint.NewImageOp(snap.frame)
		ui.renderedID = snap.frameID
	}
	widget.Image{
		Src:      ui.imageOp,
		Fit:      widget.Fill,
		Position: layout.Center,
		Scale:    1 / float32(gtx.Metric.PxPerDp),
	}.Layout(cgtx)

	hitArea := clip.Rect{Max: display.Size()}.Push(gtx.Ops)
	event.Op(gtx.Ops, &ui.remoteTag)
	pointer.CursorCrosshair.Add(gtx.Ops)
	hitArea.Pop()
	stack.Pop()

	paint.FillShape(gtx.Ops, color.NRGBA{R: 69, G: 78, B: 88, A: 255}, clip.Rect(image.Rect(display.Min.X, display.Min.Y, display.Max.X, display.Min.Y+1)).Op())
	paint.FillShape(gtx.Ops, color.NRGBA{R: 69, G: 78, B: 88, A: 255}, clip.Rect(image.Rect(display.Min.X, display.Max.Y-1, display.Max.X, display.Max.Y)).Op())
	paint.FillShape(gtx.Ops, color.NRGBA{R: 69, G: 78, B: 88, A: 255}, clip.Rect(image.Rect(display.Min.X, display.Min.Y, display.Min.X+1, display.Max.Y)).Op())
	paint.FillShape(gtx.Ops, color.NRGBA{R: 69, G: 78, B: 88, A: 255}, clip.Rect(image.Rect(display.Max.X-1, display.Min.Y, display.Max.X, display.Max.Y)).Op())

	return layout.Dimensions{Size: size}
}

func (ui *uiState) layoutFooter(gtx layout.Context, snap snapshot) layout.Dimensions {
	return layout.Inset{Top: 10, Bottom: 12, Left: 16, Right: 16}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		left := snap.status
		if left == "" {
			left = "idle"
		}
		right := snap.control
		if right == "" {
			right = "click inside the remote view to send pointer and key events"
		}
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(ui.th, left)
				l.Color = color.NRGBA{R: 188, G: 198, B: 209, A: 255}
				l.MaxLines = 1
				return l.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Width: 16}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(ui.th, right)
				l.Alignment = text.End
				l.Color = color.NRGBA{R: 126, G: 172, B: 224, A: 255}
				l.MaxLines = 1
				return l.Layout(gtx)
			}),
		)
	})
}

func (ui *uiState) handleRemoteInput(gtx layout.Context) {
	if ui.display.X <= 0 || ui.display.Y <= 0 || ui.imageSize.X <= 0 || ui.imageSize.Y <= 0 {
		return
	}
	for {
		e, ok := gtx.Event(pointer.Filter{
			Target:  &ui.remoteTag,
			Kinds:   pointer.Press | pointer.Release | pointer.Move | pointer.Drag | pointer.Scroll | pointer.Cancel,
			ScrollX: pointer.ScrollRange{Min: -2000, Max: 2000},
			ScrollY: pointer.ScrollRange{Min: -2000, Max: 2000},
		})
		if !ok {
			break
		}
		ev, ok := e.(pointer.Event)
		if !ok {
			continue
		}
		if ev.Kind == pointer.Press {
			gtx.Execute(key.FocusCmd{Tag: &ui.remoteTag})
			gtx.Execute(pointer.GrabCmd{Tag: &ui.remoteTag, ID: ev.PointerID})
		}
		x := clampInt(int(math.Round(float64(ev.Position.X)*float64(ui.imageSize.X)/float64(ui.display.X))), 0, ui.imageSize.X-1)
		y := clampInt(int(math.Round(float64(ev.Position.Y)*float64(ui.imageSize.Y)/float64(ui.display.Y))), 0, ui.imageSize.Y-1)
		buttonMask := int(ev.Buttons)
		if buttonMask == 0 && ev.Kind == pointer.Release {
			buttonMask = ui.lastButtonMask
		}
		if ev.Buttons != 0 {
			ui.lastButtonMask = int(ev.Buttons)
		} else if ev.Kind == pointer.Release || ev.Kind == pointer.Cancel {
			ui.lastButtonMask = 0
		}
		ui.sendControl(controlEvent{
			Type:       "pointer",
			Kind:       ev.Kind.String(),
			X:          x,
			Y:          y,
			Buttons:    ev.Buttons.String(),
			ButtonMask: buttonMask,
			ScrollX:    int(ev.Scroll.X),
			ScrollY:    int(ev.Scroll.Y),
		})
	}
	for {
		e, ok := gtx.Event(key.FocusFilter{Target: &ui.remoteTag}, key.Filter{
			Focus:    &ui.remoteTag,
			Optional: key.ModCtrl | key.ModCommand | key.ModShift | key.ModAlt | key.ModSuper,
		})
		if !ok {
			break
		}
		switch ev := e.(type) {
		case key.Event:
			ui.sendControl(controlEvent{
				Type:      "key",
				Kind:      ev.State.String(),
				Key:       string(ev.Name),
				Modifiers: ev.Modifiers.String(),
			})
		}
	}
}

func (ui *uiState) startSession() {
	ui.mu.Lock()
	if ui.starting || ui.session != nil {
		ui.mu.Unlock()
		return
	}
	ui.starting = true
	ui.status = "connecting to " + ui.serverURL
	ui.control = ""
	ui.mu.Unlock()
	ui.w.Invalidate()

	go func() {
		s, err := newClientSession(ui, ui.serverURL)
		ui.mu.Lock()
		defer ui.mu.Unlock()
		ui.starting = false
		if err != nil {
			ui.status = "start failed: " + err.Error()
			ui.w.Invalidate()
			return
		}
		ui.session = s
		ui.status = "client WebRTC session established"
		ui.w.Invalidate()
	}()
}

func (ui *uiState) stopSession() {
	ui.mu.Lock()
	s := ui.session
	ui.session = nil
	ui.starting = false
	ui.status = "stopped"
	ui.mu.Unlock()
	if s != nil {
		s.close()
	}
	ui.w.Invalidate()
}

func (ui *uiState) sendControl(evt controlEvent) {
	ui.mu.RLock()
	s := ui.session
	ui.mu.RUnlock()
	if s != nil {
		s.sendControl(evt)
	}
}

func (ui *uiState) setStatus(status string) {
	ui.mu.Lock()
	ui.status = status
	ui.mu.Unlock()
	ui.w.Invalidate()
}

func (ui *uiState) setControl(msg string) {
	ui.mu.Lock()
	ui.control = msg
	ui.mu.Unlock()
	ui.w.Invalidate()
}

func (ui *uiState) setFrame(img *image.NRGBA) {
	ui.mu.Lock()
	ui.frame = img
	ui.frameID++
	ui.mu.Unlock()
	ui.w.Invalidate()
}

func (ui *uiState) snapshot() snapshot {
	ui.mu.RLock()
	snap := snapshot{
		starting: ui.starting,
		running:  ui.session != nil,
		status:   ui.status,
		control:  ui.control,
		frame:    ui.frame,
		frameID:  ui.frameID,
	}
	s := ui.session
	ui.mu.RUnlock()
	if s != nil {
		snap.decoded = s.decoded.Load()
		if elapsed := time.Since(s.started).Seconds(); elapsed > 0 {
			snap.fps = float64(snap.decoded) / elapsed
		}
		snap.sent = s.sent.Load()
		snap.received = s.received.Load()
	}
	return snap
}

type clientSession struct {
	ctx     context.Context
	cancel  context.CancelFunc
	ui      *uiState
	pc      *webrtc.PeerConnection
	started time.Time

	controlMu sync.RWMutex
	control   *webrtc.DataChannel

	decoded  atomic.Uint64
	sent     atomic.Uint64
	received atomic.Uint64
}

func newClientSession(ui *uiState, serverURL string) (_ *clientSession, err error) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &clientSession{
		ctx:     ctx,
		cancel:  cancel,
		ui:      ui,
		started: time.Now(),
	}

	s.pc, err = webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		cancel()
		return nil, err
	}
	defer func() {
		if err != nil {
			s.close()
		}
	}()

	_, err = s.pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	})
	if err != nil {
		return nil, err
	}

	control, err := s.pc.CreateDataChannel("input", nil)
	if err != nil {
		return nil, err
	}
	s.control = control
	control.OnOpen(func() {
		ui.setStatus("input channel open")
	})
	control.OnClose(func() {
		s.controlMu.Lock()
		if s.control == control {
			s.control = nil
		}
		s.controlMu.Unlock()
		ui.setStatus("input channel closed")
	})
	control.OnMessage(func(msg webrtc.DataChannelMessage) {
		s.received.Add(1)
		text := string(msg.Data)
		if len(text) > 140 {
			text = text[:140]
		}
		ui.setControl("server: " + text)
	})

	s.pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		ui.setStatus("client peer: " + state.String())
		if terminalPeerState(state) {
			s.cancel()
		}
	})
	s.pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		ui.setStatus("receiving " + remote.Codec().MimeType)
		go s.receiveTrack(remote)
	})

	offer, err := s.pc.CreateOffer(nil)
	if err != nil {
		return nil, err
	}
	gather := webrtc.GatheringCompletePromise(s.pc)
	if err := s.pc.SetLocalDescription(offer); err != nil {
		return nil, err
	}
	<-gather

	answer, err := postOffer(serverURL, *s.pc.LocalDescription())
	if err != nil {
		return nil, err
	}
	if err := s.pc.SetRemoteDescription(answer); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *clientSession) close() {
	s.cancel()
	if s.pc != nil {
		_ = s.pc.Close()
	}
}

func (s *clientSession) sendControl(evt controlEvent) {
	s.controlMu.RLock()
	dc := s.control
	s.controlMu.RUnlock()
	if dc == nil || dc.ReadyState() != webrtc.DataChannelStateOpen {
		return
	}
	payload, err := json.Marshal(evt)
	if err != nil {
		return
	}
	if err := dc.SendText(string(payload)); err == nil {
		s.sent.Add(1)
	}
}

func (s *clientSession) receiveTrack(track *webrtc.TrackRemote) {
	dec, err := govpx.NewVP8Decoder(govpx.DecoderOptions{
		ErrorConcealment: true,
		MaxWidth:         desktopWidth,
		MaxHeight:        desktopHeight,
	})
	if err != nil {
		s.ui.setStatus("decoder: " + err.Error())
		return
	}
	defer dec.Close()

	builder := samplebuilder.New(32, &codecs.VP8Packet{}, track.Codec().ClockRate)
	for {
		pkt, _, err := track.ReadRTP()
		if err != nil {
			if s.ctx.Err() == nil {
				s.ui.setStatus("read RTP: " + err.Error())
			}
			return
		}
		builder.Push(pkt)
		for sample := builder.Pop(); sample != nil; sample = builder.Pop() {
			if err := dec.Decode(sample.Data); err != nil {
				if !errors.Is(err, govpx.ErrNeedKeyFrame) {
					s.ui.setStatus("decode: " + err.Error())
				}
				continue
			}
			frame, ok := dec.NextFrame()
			if !ok {
				continue
			}
			s.ui.setFrame(i420ToNRGBA(frame))
			s.decoded.Add(1)
		}
	}
}

func postOffer(server string, offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	endpoint, err := offerEndpoint(server)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	payload, err := json.Marshal(offer)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return webrtc.SessionDescription{}, fmt.Errorf("server offer failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var answer webrtc.SessionDescription
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		return webrtc.SessionDescription{}, err
	}
	return answer, nil
}

func offerEndpoint(server string) (string, error) {
	if !strings.Contains(server, "://") {
		server = "http://" + server
	}
	u, err := url.Parse(server)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported server URL scheme %q", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/offer"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

type remoteHTTPServer struct{}

func runServer(addr string) error {
	s := remoteHTTPServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/offer", s.handleOffer)
	log.Printf("govpx remote server listening on %s", listenURL(addr))
	return http.ListenAndServe(addr, mux)
}

func listenURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}
	return "http://" + addr
}

func (remoteHTTPServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, browserHTML)
}

func (remoteHTTPServer) handleOffer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var offer webrtc.SessionDescription
	if err := json.NewDecoder(r.Body).Decode(&offer); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	session, answer, err := newServerSession(offer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = session
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(answer)
}

type serverSession struct {
	ctx    context.Context
	cancel context.CancelFunc
	pc     *webrtc.PeerConnection
	source desktopSource
	input  inputSink

	forceKey atomic.Bool
	encoded  atomic.Uint64
	dropped  atomic.Uint64
	received atomic.Uint64
}

func newServerSession(offer webrtc.SessionDescription) (_ *serverSession, _ webrtc.SessionDescription, err error) {
	ctx, cancel := context.WithCancel(context.Background())
	source, err := newDefaultDesktopSource()
	if err != nil {
		cancel()
		return nil, webrtc.SessionDescription{}, err
	}
	input, err := newDefaultInputSink(source)
	if err != nil {
		cancel()
		return nil, webrtc.SessionDescription{}, err
	}
	s := &serverSession{
		ctx:    ctx,
		cancel: cancel,
		source: source,
		input:  input,
	}
	s.forceKey.Store(true)

	s.pc, err = webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		cancel()
		_ = input.Close()
		return nil, webrtc.SessionDescription{}, err
	}
	defer func() {
		if err != nil {
			s.close()
		}
	}()

	s.pc.OnDataChannel(s.handleDataChannel)
	s.pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("client peer: %s", state)
		if terminalPeerState(state) {
			s.close()
		}
	})

	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: clockRate},
		"desktop", "govpx-gio",
	)
	if err != nil {
		return nil, webrtc.SessionDescription{}, err
	}
	sender, err := s.pc.AddTrack(track)
	if err != nil {
		return nil, webrtc.SessionDescription{}, err
	}

	if err = s.pc.SetRemoteDescription(offer); err != nil {
		return nil, webrtc.SessionDescription{}, err
	}
	answer, err := s.pc.CreateAnswer(nil)
	if err != nil {
		return nil, webrtc.SessionDescription{}, err
	}
	gather := webrtc.GatheringCompletePromise(s.pc)
	if err = s.pc.SetLocalDescription(answer); err != nil {
		return nil, webrtc.SessionDescription{}, err
	}
	<-gather

	go drainRTCP(ctx, sender, &s.forceKey)
	go s.runEncoder(track)

	return s, *s.pc.LocalDescription(), nil
}

func (s *serverSession) close() {
	s.cancel()
	if s.input != nil {
		_ = s.input.Close()
	}
	if s.pc != nil {
		_ = s.pc.Close()
	}
}

func (s *serverSession) handleDataChannel(dc *webrtc.DataChannel) {
	if dc.Label() != "input" {
		return
	}
	dc.OnOpen(func() {
		log.Printf("input channel open")
	})
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		s.received.Add(1)
		var evt controlEvent
		if err := json.Unmarshal(msg.Data, &evt); err != nil {
			_ = dc.SendText("bad input: " + err.Error())
			return
		}
		if err := s.input.Handle(evt); err != nil {
			_ = dc.SendText("input error: " + err.Error())
			return
		}
		_ = dc.SendText("injected: " + evt.Summary())
	})
	dc.OnClose(func() {
		log.Printf("input channel closed")
	})
}

func (s *serverSession) runEncoder(track *webrtc.TrackLocalStaticSample) {
	size := s.source.Size()
	enc, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               size.X,
		Height:              size.Y,
		FPS:                 framerate,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   targetKbps,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  50,
		KeyFrameInterval:    framerate * 2,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             -8,
		ErrorResilient:      true,
		ScreenContentMode:   2,
		StaticThreshold:     80,
	})
	if err != nil {
		log.Printf("encoder: %v", err)
		return
	}
	defer enc.Close()

	img := newI420(size.X, size.Y)
	packet := make([]byte, 2*1024*1024)
	interval := time.Second / framerate
	duration := uint64(clockRate / framerate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var pts uint64
	var frame int
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}

		if err := s.source.Capture(img, frame); err != nil {
			log.Printf("capture: %v", err)
			return
		}
		frame++

		var flags govpx.EncodeFlags
		if s.forceKey.Swap(false) {
			flags |= govpx.EncodeForceKeyFrame
		}

		result, err := enc.EncodeInto(packet, img, pts, duration, flags)
		if err != nil {
			log.Printf("encode: %v", err)
			return
		}
		pts += duration
		if result.Dropped {
			s.dropped.Add(1)
			continue
		}

		if err := track.WriteSample(media.Sample{Data: result.Data, Duration: interval}); err != nil {
			if !errors.Is(err, io.ErrClosedPipe) && s.ctx.Err() == nil {
				log.Printf("write sample: %v", err)
			}
			return
		}
		s.encoded.Add(1)
	}
}

func drainRTCP(ctx context.Context, sender *webrtc.RTPSender, forceKey *atomic.Bool) {
	for {
		if ctx.Err() != nil {
			return
		}
		packets, _, err := sender.ReadRTCP()
		if err != nil {
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				log.Printf("rtcp read: %v", err)
			}
			return
		}
		for _, pkt := range packets {
			switch pkt.(type) {
			case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
				forceKey.Store(true)
			}
		}
	}
}

func terminalPeerState(state webrtc.PeerConnectionState) bool {
	switch state {
	case webrtc.PeerConnectionStateClosed,
		webrtc.PeerConnectionStateDisconnected,
		webrtc.PeerConnectionStateFailed:
		return true
	default:
		return false
	}
}

type controlEvent struct {
	Type       string `json:"type"`
	Kind       string `json:"kind"`
	X          int    `json:"x,omitempty"`
	Y          int    `json:"y,omitempty"`
	Buttons    string `json:"buttons,omitempty"`
	ButtonMask int    `json:"button_mask,omitempty"`
	ScrollX    int    `json:"scroll_x,omitempty"`
	ScrollY    int    `json:"scroll_y,omitempty"`
	Key        string `json:"key,omitempty"`
	Modifiers  string `json:"modifiers,omitempty"`
}

func (evt controlEvent) Summary() string {
	switch evt.Type {
	case "pointer":
		return fmt.Sprintf("%s x=%d y=%d buttons=%s", evt.Kind, evt.X, evt.Y, evt.Buttons)
	case "key":
		return fmt.Sprintf("%s key=%s mods=%s", evt.Kind, evt.Key, evt.Modifiers)
	default:
		return evt.Type
	}
}

type inputSink interface {
	Handle(controlEvent) error
	Close() error
}

func newI420(w, h int) govpx.Image {
	uvW := (w + 1) / 2
	uvH := (h + 1) / 2
	return govpx.Image{
		Width:   w,
		Height:  h,
		Y:       make([]byte, w*h),
		U:       make([]byte, uvW*uvH),
		V:       make([]byte, uvW*uvH),
		YStride: w,
		UStride: uvW,
		VStride: uvW,
	}
}

type desktopSource interface {
	Size() image.Point
	Capture(dst govpx.Image, frame int) error
}

type desktopPointMapper interface {
	MapPoint(x, y int) (float64, float64)
}

type syntheticDesktopSource struct {
	size image.Point
}

func (s syntheticDesktopSource) Size() image.Point {
	return s.size
}

func (s syntheticDesktopSource) Capture(dst govpx.Image, frame int) error {
	if dst.Width != s.size.X || dst.Height != s.size.Y {
		return fmt.Errorf("desktop source needs %dx%d frame, got %dx%d", s.size.X, s.size.Y, dst.Width, dst.Height)
	}
	drawSyntheticDesktop(dst, frame)
	return nil
}

func drawSyntheticDesktop(img govpx.Image, t int) {
	fillRect(img, 0, 0, img.Width, img.Height, 38, 114, 140)
	fillRect(img, 0, 0, img.Width, 42, 52, 104, 166)
	fillRect(img, 0, img.Height-40, img.Width, img.Height, 28, 124, 132)
	fillRect(img, 22, 62, 244, img.Height-62, 46, 100, 151)
	fillRect(img, 262, 62, img.Width-28, img.Height-62, 222, 128, 128)
	fillRect(img, 262, 62, img.Width-28, 96, 74, 92, 170)
	fillRect(img, 286, 122, img.Width-54, 174, 206, 108, 142)
	fillRect(img, 286, 200, img.Width-54, img.Height-92, 236, 128, 128)
	fillRect(img, 304, 220, img.Width-72, img.Height-112, 28, 110, 146)

	for i := 0; i < 7; i++ {
		y := 82 + i*42
		fillRect(img, 42, y, 224, y+22, byte(74+i*8), 118, 146)
	}
	for i := 0; i < 8; i++ {
		y := 236 + i*21
		fillRect(img, 324, y, 800, y+2, 82, 118, 150)
		fillRect(img, 324, y+8, 600+((i+t)%5)*38, y+12, 170, 112, 136)
	}
	for i := 0; i < 5; i++ {
		x := 302 + i*96
		fillRect(img, x, 138, x+58, 154, byte(120+i*18), 90, 170)
	}

	cursorX := 310 + (t*11)%(img.Width-410)
	cursorY := 160 + int(90*math.Sin(float64(t)*0.08))
	drawCursor(img, cursorX, cursorY)
}

func fillRect(img govpx.Image, x0, y0, x1, y1 int, yy, uu, vv byte) {
	x0 = clampInt(x0, 0, img.Width)
	x1 = clampInt(x1, 0, img.Width)
	y0 = clampInt(y0, 0, img.Height)
	y1 = clampInt(y1, 0, img.Height)
	if x1 <= x0 || y1 <= y0 {
		return
	}
	for y := y0; y < y1; y++ {
		row := img.Y[y*img.YStride+x0 : y*img.YStride+x1]
		for i := range row {
			row[i] = yy
		}
	}

	uvX0 := x0 / 2
	uvX1 := (x1 + 1) / 2
	uvY0 := y0 / 2
	uvY1 := (y1 + 1) / 2
	for y := uvY0; y < uvY1; y++ {
		uRow := img.U[y*img.UStride+uvX0 : y*img.UStride+uvX1]
		vRow := img.V[y*img.VStride+uvX0 : y*img.VStride+uvX1]
		for i := range uRow {
			uRow[i] = uu
			vRow[i] = vv
		}
	}
}

func drawCursor(img govpx.Image, x, y int) {
	for dy := 0; dy < 24; dy++ {
		for dx := 0; dx <= dy/2; dx++ {
			px := x + dx
			py := y + dy
			if px >= 0 && px < img.Width && py >= 0 && py < img.Height {
				img.Y[py*img.YStride+px] = 245
			}
		}
	}
	fillRect(img, x+8, y+17, x+14, y+31, 235, 128, 128)
	fillRect(img, x+1, y+1, x+4, y+22, 18, 128, 128)
}

func i420ToNRGBA(src govpx.Image) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, src.Width, src.Height))
	for y := 0; y < src.Height; y++ {
		yRow := src.Y[y*src.YStride : y*src.YStride+src.Width]
		uRow := src.U[(y/2)*src.UStride:]
		vRow := src.V[(y/2)*src.VStride:]
		out := dst.Pix[y*dst.Stride : y*dst.Stride+src.Width*4]
		for x := 0; x < src.Width; x++ {
			yy := int(yRow[x])
			cb := int(uRow[x/2]) - 128
			cr := int(vRow[x/2]) - 128
			r := yy + ((91881 * cr) >> 16)
			g := yy - ((22554*cb + 46802*cr) >> 16)
			b := yy + ((116130 * cb) >> 16)
			i := x * 4
			out[i+0] = byte(clampInt(r, 0, 255))
			out[i+1] = byte(clampInt(g, 0, 255))
			out[i+2] = byte(clampInt(b, 0, 255))
			out[i+3] = 255
		}
	}
	return dst
}

func contain(bounds image.Point, content image.Point) image.Rectangle {
	if bounds.X <= 0 || bounds.Y <= 0 || content.X <= 0 || content.Y <= 0 {
		return image.Rectangle{}
	}
	scale := math.Min(float64(bounds.X)/float64(content.X), float64(bounds.Y)/float64(content.Y))
	w := int(math.Round(float64(content.X) * scale))
	h := int(math.Round(float64(content.Y) * scale))
	x := (bounds.X - w) / 2
	y := (bounds.Y - h) / 2
	return image.Rect(x, y, x+w, y+h)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
