// gopvx WebRTC demo: generate synthetic frames in Go, encode VP8 with
// gopvx, stream to a browser over WebRTC. Run with `go run .` and open
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
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"github.com/thesyncim/gopvx"
)

const (
	frameWidth  = 320
	frameHeight = 240
	framerate   = 30
)

const indexHTML = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>gopvx VP8 over WebRTC</title>
<style>
body{font-family:system-ui,sans-serif;margin:24px;max-width:720px}
video{width:100%;background:#111;border-radius:6px}
pre{background:#f4f4f4;padding:8px;border-radius:4px}
</style></head>
<body>
<h1>gopvx VP8 over WebRTC</h1>
<p>Frames are generated in Go, encoded with gopvx (pure-Go VP8), and decoded by your browser's VP8 decoder.</p>
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
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, indexHTML)
	})
	mux.HandleFunc("/offer", handleOffer)

	log.Printf("listening on http://localhost%s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

func handleOffer(w http.ResponseWriter, r *http.Request) {
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
		"gopvx-video", "gopvx",
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

	go runEncoder(ctx, track, &forceKey)

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

func runEncoder(ctx context.Context, track *webrtc.TrackLocalStaticSample, forceKey *atomic.Bool) {
	enc, err := gopvx.NewVP8Encoder(gopvx.EncoderOptions{
		Width:               frameWidth,
		Height:              frameHeight,
		FPS:                 framerate,
		RateControlMode:     gopvx.RateControlCBR,
		TargetBitrateKbps:   600,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		KeyFrameInterval:    framerate * 2,
		Deadline:            gopvx.DeadlineRealtime,
		ErrorResilient:      true,
	})
	if err != nil {
		log.Printf("NewVP8Encoder: %v", err)
		return
	}
	defer enc.Close()

	img := newImage(frameWidth, frameHeight)
	packet := make([]byte, 256*1024)
	interval := time.Second / framerate
	duration := uint64(90000 / framerate)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var pts uint64
	var t int
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		drawFrame(img, t)
		t++

		var flags gopvx.EncodeFlags
		if forceKey.Swap(false) {
			flags |= gopvx.EncodeForceKeyFrame
		}

		result, err := enc.EncodeInto(packet, img, pts, duration, flags)
		if err != nil {
			log.Printf("EncodeInto: %v", err)
			return
		}
		pts += duration
		if result.Dropped {
			continue
		}
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

func newImage(w, h int) gopvx.Image {
	uvW := (w + 1) / 2
	uvH := (h + 1) / 2
	return gopvx.Image{
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
func drawFrame(img gopvx.Image, t int) {
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
