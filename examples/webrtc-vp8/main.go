// govpx WebRTC demo: generate synthetic frames in Go, encode VP8 with
// govpx, stream to a browser over WebRTC. Run with `go run .` and open
// http://localhost:8080.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"github.com/thesyncim/govpx"
)

const (
	defaultFrameWidth  = 320
	defaultFrameHeight = 240
	defaultFramerate   = 30
	defaultBitrateKbps = 600
	rtpClockHz         = 90000
)

type demoConfig struct {
	Addr        string
	Width       int
	Height      int
	FPS         int
	BitrateKbps int
}

const indexHTML = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>govpx VP8 over WebRTC</title>
<style>
body{font-family:system-ui,sans-serif;margin:24px;max-width:720px}
video{width:100%;background:#111;border-radius:6px}
pre{background:#f4f4f4;padding:8px;border-radius:4px}
</style></head>
<body>
<h1>govpx VP8 over WebRTC</h1>
<p>Frames are generated in Go, encoded with govpx (pure-Go VP8), and decoded by your browser's VP8 decoder.</p>
<video id="v" autoplay playsinline muted controls></video>
<pre id="status">connecting…</pre>
<script>
async function start(){
  const status = document.getElementById("status");
  const pc = new RTCPeerConnection();
  pc.addTransceiver("video",{direction:"recvonly"});
  pc.ontrack = e => { document.getElementById("v").srcObject = e.streams[0]; };
  pc.oniceconnectionstatechange = () => { status.textContent = "ICE: " + pc.iceConnectionState; };
  pc.onconnectionstatechange = () => { status.textContent = "peer: " + pc.connectionState; };
  await pc.setLocalDescription(await pc.createOffer());
  await new Promise(r => {
    if (pc.iceGatheringState === "complete") return r();
    pc.onicegatheringstatechange = () => pc.iceGatheringState === "complete" && r();
  });
  const res = await fetch("/offer", {
    method: "POST",
    headers: {"Content-Type":"application/json"},
    body: JSON.stringify(pc.localDescription),
  });
  if (!res.ok) { status.textContent = "offer failed: " + res.status; return; }
  await pc.setRemoteDescription(await res.json());
}
start().catch(e => document.getElementById("status").textContent = "error: " + e);
</script>
</body></html>`

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	width := flag.Int("width", defaultFrameWidth, "encoded video width")
	height := flag.Int("height", defaultFrameHeight, "encoded video height")
	fps := flag.Int("fps", defaultFramerate, "encoded frame rate")
	bitrate := flag.Int("bitrate", defaultBitrateKbps, "target bitrate in kbps")
	flag.Parse()
	if *width <= 0 || *height <= 0 || *fps <= 0 || *bitrate <= 0 {
		log.Fatal("width, height, fps, and bitrate must be positive")
	}
	cfg := demoConfig{
		Addr:        *addr,
		Width:       *width,
		Height:      *height,
		FPS:         *fps,
		BitrateKbps: *bitrate,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, indexHTML)
	})
	mux.HandleFunc("/offer", func(w http.ResponseWriter, r *http.Request) {
		handleOffer(w, r, cfg)
	})

	log.Printf("listening on http://localhost%s (%dx%d @ %dfps, %dkbps)", cfg.Addr, cfg.Width, cfg.Height, cfg.FPS, cfg.BitrateKbps)
	if err := http.ListenAndServe(cfg.Addr, mux); err != nil {
		log.Fatal(err)
	}
}

func handleOffer(w http.ResponseWriter, r *http.Request, cfg demoConfig) {
	var offer webrtc.SessionDescription
	if err := json.NewDecoder(r.Body).Decode(&offer); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000},
		"govpx-video", "govpx",
	)
	if err != nil {
		_ = pc.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sender, err := pc.AddTrack(track)
	if err != nil {
		_ = pc.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := pc.SetRemoteDescription(offer); err != nil {
		_ = pc.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		_ = pc.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	gather := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		_ = pc.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	<-gather

	ctx, cancel := context.WithCancel(context.Background())
	var forceKey atomic.Bool
	forceKey.Store(true)

	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		log.Printf("peer connection state: %s", s)
		switch s {
		case webrtc.PeerConnectionStateClosed,
			webrtc.PeerConnectionStateFailed,
			webrtc.PeerConnectionStateDisconnected:
			cancel()
			_ = pc.Close()
		}
	})

	// Read RTCP feedback (PLI/FIR) from the sender; on receipt, force a keyframe.
	go drainRTCP(ctx, sender, &forceKey)

	go runEncoder(ctx, track, &forceKey, cfg)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pc.LocalDescription())
}

func drainRTCP(ctx context.Context, sender *webrtc.RTPSender, forceKey *atomic.Bool) {
	buf := make([]byte, 1500)
	for {
		if ctx.Err() != nil {
			return
		}
		n, _, err := sender.Read(buf)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("rtcp read: %v", err)
			}
			return
		}
		// Any feedback is a hint to refresh; for a demo we simply force a keyframe.
		_ = n
		forceKey.Store(true)
	}
}

func runEncoder(ctx context.Context, track *webrtc.TrackLocalStaticSample, forceKey *atomic.Bool, cfg demoConfig) {
	maxIntraPct := webrtcMaxIntraTargetPct(600, cfg.FPS)
	enc, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               cfg.Width,
		Height:              cfg.Height,
		FPS:                 cfg.FPS,
		Threads:             webrtcVP8ThreadCount(cfg.Width, cfg.Height),
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   cfg.BitrateKbps,
		MinQuantizer:        2,
		MaxQuantizer:        56,
		UndershootPct:       100,
		OvershootPct:        15,
		BufferSizeMs:        1000,
		BufferInitialSizeMs: 500,
		BufferOptimalSizeMs: 600,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  30,
		MaxIntraBitratePct:  maxIntraPct,
		KeyFrameInterval:    3000,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             webrtcVP8CPUUsed(cfg.Width, cfg.Height),
		NoiseSensitivity:    4,
		StaticThreshold:     1,
	})
	if err != nil {
		log.Printf("NewVP8Encoder: %v", err)
		return
	}
	defer enc.Close()

	img := newImage(cfg.Width, cfg.Height)
	packet := make([]byte, outputBufferSize(cfg.Width, cfg.Height))
	interval := time.Second / time.Duration(cfg.FPS)
	duration := uint64(rtpClockHz / cfg.FPS)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var pts uint64
	var t int
	windowStart := time.Now()
	var windowBytes int
	var windowFrames int
	var windowDrops int
	var lastQ int
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		drawFrame(img, t)
		t++

		var flags govpx.EncodeFlags
		if forceKey.Swap(false) {
			flags |= govpx.EncodeForceKeyFrame
		}

		result, err := enc.EncodeInto(packet, img, pts, duration, flags)
		if err != nil {
			log.Printf("EncodeInto: %v", err)
			return
		}
		pts += duration
		if result.Dropped {
			windowDrops++
			logEncoderWindow(cfg, &windowStart, &windowBytes, &windowFrames, &windowDrops, lastQ)
			continue
		}
		windowBytes += result.SizeBytes
		windowFrames++
		lastQ = result.Quantizer
		logEncoderWindow(cfg, &windowStart, &windowBytes, &windowFrames, &windowDrops, lastQ)
		if err := track.WriteSample(media.Sample{
			Data:     result.Data,
			Duration: interval,
		}); err != nil {
			if !errors.Is(err, io.ErrClosedPipe) {
				log.Printf("WriteSample: %v", err)
			}
			return
		}
	}
}

func webrtcVP8CPUUsed(width int, height int) int {
	// Mirrors libwebrtc's non-ARM default complexity: speed 4 below CIF,
	// speed 6 at CIF and above.
	if width*height < 352*288 {
		return -4
	}
	return -6
}

func webrtcVP8ThreadCount(width int, height int) int {
	// Mirrors libwebrtc's desktop VP8 thread selection for one stream.
	cpus := runtime.NumCPU()
	pixels := width * height
	switch {
	case pixels >= 1920*1080 && cpus > 8:
		return 8
	case pixels > 1280*960 && cpus >= 6:
		return 3
	case pixels > 640*480 && cpus >= 6:
		return 3
	case pixels > 640*480 && cpus >= 3:
		return 2
	default:
		return 1
	}
}

func webrtcMaxIntraTargetPct(optimalBufferMs int, fps int) int {
	target := optimalBufferMs * fps / 20
	if target < 300 {
		return 300
	}
	return target
}

func outputBufferSize(width int, height int) int {
	n := width*height*3/2 + 4096
	if n < 256*1024 {
		return 256 * 1024
	}
	return n
}

func logEncoderWindow(cfg demoConfig, start *time.Time, bytes *int, frames *int, drops *int, q int) {
	elapsed := time.Since(*start)
	if elapsed < time.Second {
		return
	}
	kbps := float64(*bytes*8) / elapsed.Seconds() / 1000
	log.Printf("encoded %.0f kbps target=%d kbps frames=%d drops=%d q=%d", kbps, cfg.BitrateKbps, *frames, *drops, q)
	*start = time.Now()
	*bytes = 0
	*frames = 0
	*drops = 0
}

func newImage(w, h int) govpx.Image {
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

// drawFrame writes a deterministic animated pattern: a slow Y-plane gradient
// scrolling horizontally with a moving bright square, and a chroma plane that
// drifts over time. The motion is purely Go code — there's no input device.
func drawFrame(img govpx.Image, t int) {
	w, h := img.Width, img.Height

	cx := (t * 4) % (w + 64)
	cy := h/2 - 32 + (t % 30)
	const boxSize = 48

	for y := 0; y < h; y++ {
		row := img.Y[y*img.YStride : y*img.YStride+w]
		for x := 0; x < w; x++ {
			v := byte(64 + ((x + t*2) & 0x7F))
			if x >= cx && x < cx+boxSize && y >= cy && y < cy+boxSize {
				v = 235
			}
			row[x] = v
		}
	}

	uvW := (w + 1) / 2
	uvH := (h + 1) / 2
	uVal := byte(128 + 40*int(sineFixed(t, 90)))
	vVal := byte(128 + 40*int(sineFixed(t+45, 90)))
	for y := 0; y < uvH; y++ {
		uRow := img.U[y*img.UStride : y*img.UStride+uvW]
		vRow := img.V[y*img.VStride : y*img.VStride+uvW]
		for x := 0; x < uvW; x++ {
			uRow[x] = uVal
			vRow[x] = vVal
		}
	}
}

// sineFixed returns a triangle-ish wave in [-1, 1] using only integer math,
// good enough for visible chroma drift.
func sineFixed(t int, period int) int8 {
	phase := ((t % period) * 4) / period
	switch phase {
	case 0:
		return 1
	case 1:
		return 1
	case 2:
		return -1
	default:
		return -1
	}
}
