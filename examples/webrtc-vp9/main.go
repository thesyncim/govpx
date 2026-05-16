// govpx WebRTC VP9 SVC demo: synthesizes frames in Go, encodes a 3-layer
// spatial-SVC VP9 superframe with 3 temporal layers per access unit using
// govpx, streams the access units to the browser over WebRTC, and ships
// per-layer telemetry over a DataChannel so the page can render a live
// overlay. Bidirectional control messages on the same DataChannel let the
// page change bitrate, screen-content tuning, force keyframes, and pause
// the encoder. Run with `go run .` and open http://localhost:8080.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"github.com/thesyncim/govpx"
)

const (
	spatialLayerCount = 3
	rtpClockHz        = 90000

	defaultFPS         = 30
	defaultBitrateKbps = 2500
)

// layerDims holds the per-spatial-layer resolution, base to top. Each step
// is 2x in each dimension, satisfying VP9 SVC inter-layer scaling rules.
var layerDims = [spatialLayerCount][2]int{
	{320, 180},
	{640, 360},
	{1280, 720},
}

// layerSplitPct holds the cumulative bitrate share of each spatial layer
// (base..top). The top entry must be 100; matches the spatial SVC profile
// used by libvpx samples for 3-layer 180p/360p/720p delivery.
var layerSplitPct = [spatialLayerCount]int{12, 48, 100}

// temporalSplitPct holds the cumulative bitrate share inside a single
// spatial layer across its three temporal layers (TL0/TL1/TL2). Mirrors
// libvpx's default 3-layer split.
var temporalSplitPct = [3]int{40, 60, 100}

type demoConfig struct {
	Addr        string
	FPS         int
	BitrateKbps int
}

const indexHTML = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>govpx VP9 SVC over WebRTC</title>
<style>
:root{color-scheme:dark light;font-family:system-ui,sans-serif}
body{margin:0;padding:18px;max-width:1100px;margin-inline:auto;display:grid;
  grid-template-columns:minmax(0,1fr) 320px;gap:18px;align-items:start}
h1{margin:0 0 8px;font-size:20px}
h2{margin:18px 0 6px;font-size:14px;text-transform:uppercase;letter-spacing:.05em;opacity:.7}
video{width:100%;background:#111;border-radius:8px;display:block}
.card{background:rgba(127,127,127,.08);border:1px solid rgba(127,127,127,.18);
  padding:12px 14px;border-radius:8px}
.layers{display:grid;grid-template-columns:1fr 1fr 1fr;gap:8px}
.layer{padding:8px;border-radius:6px;background:rgba(127,127,127,.08);
  font-variant-numeric:tabular-nums;font-size:12px;line-height:1.4}
.layer.k{outline:2px solid #ffcb6b}
.layer .res{font-weight:600;font-size:13px;margin-bottom:4px}
.layer dl{display:grid;grid-template-columns:auto 1fr;gap:2px 8px;margin:0}
.layer dt{opacity:.65}
.layer dd{margin:0;text-align:right}
.totals{display:grid;grid-template-columns:1fr 1fr;gap:6px 16px;margin-top:8px;font-size:13px}
.totals dt{opacity:.65}
.totals dd{margin:0;text-align:right;font-variant-numeric:tabular-nums}
canvas{display:block;width:100%;height:72px;background:rgba(127,127,127,.06);border-radius:6px}
button,input[type=range]{font:inherit}
button{padding:6px 10px;border-radius:6px;border:1px solid rgba(127,127,127,.3);
  background:rgba(127,127,127,.1);cursor:pointer}
button:hover{background:rgba(127,127,127,.2)}
button.on{outline:2px solid #82aaff}
.row{display:flex;gap:6px;flex-wrap:wrap;align-items:center;margin:6px 0}
.row label{font-size:12px;opacity:.7;min-width:60px}
pre{margin:0;font-size:11px;white-space:pre-wrap;word-break:break-word}
#status{font-size:12px;opacity:.7;margin-top:6px}
</style></head>
<body>
<main>
  <h1>govpx VP9 SVC over WebRTC</h1>
  <p style="margin:0 0 10px;opacity:.75;font-size:13px">
    Three spatial layers (180p/360p/720p) with three temporal layers each, packed
    into a VP9 superframe per access unit, encoded in pure Go by govpx and
    decoded by the browser's native VP9 decoder. Stats and controls travel a
    DataChannel.
  </p>
  <video id="v" autoplay playsinline muted></video>
  <div id="status">connecting&hellip;</div>
</main>
<aside>
  <div class="card">
    <h2 style="margin-top:0">Live access unit</h2>
    <div class="layers" id="layers">
      <div class="layer" data-layer="0"><div class="res">320x180</div><dl></dl></div>
      <div class="layer" data-layer="1"><div class="res">640x360</div><dl></dl></div>
      <div class="layer" data-layer="2"><div class="res">1280x720</div><dl></dl></div>
    </div>
    <dl class="totals" id="totals"></dl>
  </div>

  <div class="card">
    <h2>Recent bitrate (kbps)</h2>
    <canvas id="chart" width="320" height="72"></canvas>
  </div>

  <div class="card">
    <h2>Controls</h2>
    <div class="row">
      <label for="bitrate">bitrate</label>
      <input id="bitrate" type="range" min="200" max="6000" step="100" />
      <span id="bitrateLabel" style="min-width:64px;text-align:right">- kbps</span>
    </div>
    <div class="row">
      <label>tuning</label>
      <button data-screen="0" class="on">video</button>
      <button data-screen="1">screen</button>
      <button data-screen="2">film</button>
    </div>
    <div class="row">
      <button id="kf">force keyframe</button>
      <button id="pause">pause encoder</button>
    </div>
  </div>

  <div class="card">
    <h2>Last superframe</h2>
    <pre id="raw"></pre>
  </div>
</aside>
<script>
const status = document.getElementById("status");
const layersEl = document.getElementById("layers");
const totalsEl = document.getElementById("totals");
const rawEl = document.getElementById("raw");
const chart = document.getElementById("chart");
const ctx2d = chart.getContext("2d");
const samples = []; // {kbps, ts}
const MAX_SAMPLES = 240;

function row(dl, k, v){
  const dt = document.createElement("dt"); dt.textContent = k;
  const dd = document.createElement("dd"); dd.textContent = v;
  dl.appendChild(dt); dl.appendChild(dd);
}

function drawChart(){
  const w = chart.width, h = chart.height;
  ctx2d.clearRect(0,0,w,h);
  if(!samples.length) return;
  let mx = 1;
  for(const s of samples) if(s.kbps > mx) mx = s.kbps;
  mx = Math.max(mx, 100);
  ctx2d.strokeStyle = "rgba(130,170,255,.85)";
  ctx2d.fillStyle = "rgba(130,170,255,.25)";
  ctx2d.lineWidth = 1.5;
  ctx2d.beginPath();
  for(let i=0;i<samples.length;i++){
    const x = (i/(MAX_SAMPLES-1))*w;
    const y = h - (samples[i].kbps/mx)*(h-6) - 3;
    if(i===0) ctx2d.moveTo(x,y); else ctx2d.lineTo(x,y);
  }
  ctx2d.lineTo(w,h); ctx2d.lineTo(0,h); ctx2d.closePath();
  ctx2d.fill();
  ctx2d.beginPath();
  for(let i=0;i<samples.length;i++){
    const x = (i/(MAX_SAMPLES-1))*w;
    const y = h - (samples[i].kbps/mx)*(h-6) - 3;
    if(i===0) ctx2d.moveTo(x,y); else ctx2d.lineTo(x,y);
  }
  ctx2d.stroke();
  ctx2d.fillStyle = "rgba(127,127,127,.7)";
  ctx2d.font = "10px system-ui";
  ctx2d.textAlign = "right";
  ctx2d.fillText(mx.toFixed(0)+" kbps", w-4, 11);
}

function renderStats(msg){
  // per-layer panels
  for(const node of layersEl.querySelectorAll(".layer")){
    const id = +node.dataset.layer;
    const dl = node.querySelector("dl");
    dl.textContent = "";
    const layer = msg.layers[id];
    if(!layer){ node.classList.remove("k"); continue; }
    node.classList.toggle("k", !!layer.kf);
    row(dl, "q", layer.q);
    row(dl, "bytes", layer.bytes);
    row(dl, "kbps", (layer.kbps_recent||0).toFixed(0));
    row(dl, "T", layer.tp + (layer.sync?"↑":""));
    row(dl, "TL0", layer.tl0);
    if(layer.dropped) row(dl, "drop", "yes");
  }
  // totals
  totalsEl.textContent = "";
  row(totalsEl, "frame #", msg.frame);
  row(totalsEl, "ts (ms)", (msg.ts_ms||0).toFixed(0));
  row(totalsEl, "AU bytes", msg.totals.bytes);
  row(totalsEl, "AU kbps", (msg.totals.kbps_recent||0).toFixed(0));
  row(totalsEl, "fps", (msg.totals.fps||0).toFixed(1));
  row(totalsEl, "target kbps", msg.settings.target_kbps);
  row(totalsEl, "screen mode", ["video","screen","film"][msg.settings.screen_mode] || "?");
  if(msg.ss_present) row(totalsEl, "SS", "present");
  // chart
  samples.push({kbps: msg.totals.kbps_recent||0});
  if(samples.length > MAX_SAMPLES) samples.shift();
  drawChart();
  rawEl.textContent = JSON.stringify(msg, null, 2);
}

let dc = null;
function sendCtl(obj){ if(dc && dc.readyState === "open") dc.send(JSON.stringify(obj)); }

document.getElementById("kf").onclick = () => sendCtl({type:"keyframe"});
const pauseBtn = document.getElementById("pause");
let paused = false;
pauseBtn.onclick = () => {
  paused = !paused;
  pauseBtn.classList.toggle("on", paused);
  pauseBtn.textContent = paused ? "resume encoder" : "pause encoder";
  sendCtl({type:"pause", paused});
};

for(const b of document.querySelectorAll("button[data-screen]")){
  b.onclick = () => {
    for(const o of document.querySelectorAll("button[data-screen]")) o.classList.remove("on");
    b.classList.add("on");
    sendCtl({type:"screen", mode:+b.dataset.screen});
  };
}

const bitrate = document.getElementById("bitrate");
const bitrateLabel = document.getElementById("bitrateLabel");
let bitrateTimer = 0;
bitrate.oninput = () => {
  bitrateLabel.textContent = bitrate.value + " kbps";
  clearTimeout(bitrateTimer);
  bitrateTimer = setTimeout(() => sendCtl({type:"bitrate", kbps:+bitrate.value}), 80);
};

async function start(){
  const pc = new RTCPeerConnection({iceServers:[{urls:"stun:stun.l.google.com:19302"}]});
  pc.addTransceiver("video",{direction:"recvonly"});
  pc.ontrack = e => { document.getElementById("v").srcObject = e.streams[0]; };
  pc.oniceconnectionstatechange = () => { status.textContent = "ICE: " + pc.iceConnectionState; };
  pc.onconnectionstatechange = () => { status.textContent = "peer: " + pc.connectionState; };
  dc = pc.createDataChannel("demo", {ordered:true});
  dc.onopen = () => { status.textContent += " | dc open"; };
  dc.onmessage = ev => {
    try { renderStats(JSON.parse(ev.data)); }
    catch(err){ console.error(err); }
  };
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
start().catch(e => status.textContent = "error: " + e);
</script>
</body></html>`

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	fps := flag.Int("fps", defaultFPS, "encoded frame rate")
	bitrate := flag.Int("bitrate", defaultBitrateKbps, "total target bitrate in kbps")
	flag.Parse()
	if *fps <= 0 || *bitrate <= 0 {
		log.Fatal("fps and bitrate must be positive")
	}
	cfg := demoConfig{Addr: *addr, FPS: *fps, BitrateKbps: *bitrate}

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

	log.Printf("listening on http://localhost%s (3 spatial layers up to %dx%d @ %dfps, %dkbps total)",
		cfg.Addr, layerDims[spatialLayerCount-1][0], layerDims[spatialLayerCount-1][1],
		cfg.FPS, cfg.BitrateKbps)
	if err := http.ListenAndServe(cfg.Addr, mux); err != nil {
		log.Fatal(err)
	}
}

// controlState holds the runtime knobs the browser can adjust through the
// "demo" DataChannel. Reads happen on the encoder goroutine, writes from the
// DataChannel callback; an atomic snapshot is fine because each control is
// independent.
type controlState struct {
	bitrateKbps atomic.Int64
	screenMode  atomic.Int32
	paused      atomic.Bool
	forceKey    atomic.Bool
}

type controlMessage struct {
	Type   string `json:"type"`
	Kbps   int    `json:"kbps,omitempty"`
	Mode   int    `json:"mode,omitempty"`
	Paused bool   `json:"paused,omitempty"`
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
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP9, ClockRate: 90000},
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

	ctl := &controlState{}
	ctl.bitrateKbps.Store(int64(cfg.BitrateKbps))
	ctl.screenMode.Store(0)
	ctl.forceKey.Store(true)

	// telemetry is a tiny buffered channel. The encoder goroutine pushes one
	// stats message per access unit; the DataChannel writer drains and sends
	// it. Buffering avoids back-pressuring the encoder when the DC isn't
	// drained yet, and dropping the oldest message keeps live data fresh.
	telemetry := make(chan []byte, 4)

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != "demo" {
			return
		}
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			var m controlMessage
			if err := json.Unmarshal(msg.Data, &m); err != nil {
				log.Printf("ctl decode: %v", err)
				return
			}
			applyControl(ctl, m, cfg)
		})
		// Forward telemetry until the DC closes.
		go func() {
			for {
				select {
				case payload, ok := <-telemetry:
					if !ok {
						return
					}
					if err := dc.SendText(string(payload)); err != nil {
						return
					}
				}
			}
		}()
	})

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

	go drainRTCP(ctx, sender, ctl)
	go runEncoder(ctx, track, telemetry, ctl, cfg)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pc.LocalDescription())
}

func applyControl(ctl *controlState, m controlMessage, cfg demoConfig) {
	switch m.Type {
	case "keyframe":
		ctl.forceKey.Store(true)
	case "pause":
		ctl.paused.Store(m.Paused)
	case "screen":
		if m.Mode < 0 || m.Mode > 2 {
			return
		}
		ctl.screenMode.Store(int32(m.Mode))
	case "bitrate":
		kbps := m.Kbps
		if kbps < 100 {
			kbps = 100
		}
		if kbps > 20000 {
			kbps = 20000
		}
		ctl.bitrateKbps.Store(int64(kbps))
		_ = cfg // reserved for future per-session config tweaks
	}
}

func drainRTCP(ctx context.Context, sender *webrtc.RTPSender, ctl *controlState) {
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
		_ = n
		ctl.forceKey.Store(true)
	}
}

// runEncoder drives the spatial SVC encoder, packing one VP9 superframe per
// access unit into the track and sending a telemetry message describing every
// coded spatial/temporal layer to the browser.
func runEncoder(ctx context.Context, track *webrtc.TrackLocalStaticSample,
	telemetry chan []byte, ctl *controlState, cfg demoConfig) {
	defer close(telemetry)

	svc, err := newSVCEncoder(cfg)
	if err != nil {
		log.Printf("newSVCEncoder: %v", err)
		return
	}
	defer svc.Close()

	// Per-layer reusable image buffers. The synthetic painter writes directly
	// into these on every tick so the encoder hot path stays allocation-free.
	imgs := make([]*image.YCbCr, spatialLayerCount)
	for i := 0; i < spatialLayerCount; i++ {
		imgs[i] = image.NewYCbCr(image.Rect(0, 0, layerDims[i][0], layerDims[i][1]),
			image.YCbCrSubsampleRatio420)
	}

	// One superframe lives at the top-layer pixel budget plus framing slack;
	// VP9 keyframes at 1280x720 fit well under this.
	packet := make([]byte, superframeBudget())
	interval := time.Second / time.Duration(cfg.FPS)
	duration := uint64(rtpClockHz / cfg.FPS)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	currentBitrate := int(ctl.bitrateKbps.Load())
	currentScreen := int(ctl.screenMode.Load())

	statsTracker := newStatsTracker()
	var pts uint64
	var sceneT int
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		// Apply runtime updates between access units. We re-set them
		// lazily so the SVC encoder's per-layer rate-control stays in
		// sync without recreating internal state every frame.
		if want := int(ctl.bitrateKbps.Load()); want != currentBitrate {
			if err := applyBitrate(svc, want); err != nil {
				log.Printf("SetBitrate(%d): %v", want, err)
			} else {
				currentBitrate = want
			}
		}
		if want := int(ctl.screenMode.Load()); want != currentScreen {
			if err := applyScreenMode(svc, want); err != nil {
				log.Printf("SetScreenContentMode(%d): %v", want, err)
			} else {
				currentScreen = want
			}
		}
		if ctl.forceKey.Swap(false) {
			forceKeyAll(svc)
		}
		if ctl.paused.Load() {
			continue
		}

		sceneT++
		drawScene(imgs, sceneT)

		result, err := svc.EncodeIntoWithResult(imgs, packet)
		if err != nil {
			log.Printf("EncodeIntoWithResult: %v", err)
			return
		}
		statsTracker.observe(result, time.Now())

		// Pion handles VP9 RTP packetisation off media.Sample.Data; the
		// browser sees the entire SVC superframe as one access unit and the
		// VP9 decoder picks the highest visible spatial layer.
		if err := track.WriteSample(media.Sample{
			Data:     result.Data,
			Duration: interval,
		}); err != nil {
			if !errors.Is(err, io.ErrClosedPipe) {
				log.Printf("WriteSample: %v", err)
			}
			return
		}

		payload, err := statsTracker.snapshot(result, currentBitrate, currentScreen, pts)
		if err == nil {
			pushTelemetry(telemetry, payload)
		}
		pts += duration
	}
}

// pushTelemetry sends a payload to the DataChannel writer, dropping the
// oldest queued message under backpressure so the browser always sees fresh
// numbers rather than stale ones.
func pushTelemetry(telemetry chan []byte, payload []byte) {
	for {
		select {
		case telemetry <- payload:
			return
		default:
			select {
			case <-telemetry:
			default:
				return
			}
		}
	}
}

func superframeBudget() int {
	// Top-layer 1280x720 YUV is 1.4 MB; allow ~256 KB output plus per-layer
	// VP9 first_partition_size slack (64 KB each) on top.
	const topLayer = 1280 * 720
	return topLayer/2 + spatialLayerCount*64*1024
}

func newSVCEncoder(cfg demoConfig) (*govpx.VP9SpatialSVCEncoder, error) {
	layerBitrates := splitBitrate(cfg.BitrateKbps, layerSplitPct)

	var layers [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions
	for i := 0; i < spatialLayerCount; i++ {
		w, h := layerDims[i][0], layerDims[i][1]
		layerTarget := layerBitrates[i]
		layers[i] = govpx.VP9EncoderOptions{
			Width:               w,
			Height:              h,
			FPS:                 cfg.FPS,
			Threads:             pickThreads(w, h),
			Deadline:            govpx.DeadlineRealtime,
			CpuUsed:             pickCPUUsed(w, h),
			RateControlModeSet:  true,
			RateControlMode:     govpx.RateControlCBR,
			TargetBitrateKbps:   layerTarget,
			MinQuantizer:        2,
			MaxQuantizer:        56,
			BufferSizeMs:        1000,
			BufferInitialSizeMs: 500,
			BufferOptimalSizeMs: 600,
			OvershootPct:        20,
			UndershootPct:       100,
			MaxIntraBitratePct:  900,
			MaxKeyframeInterval: 3000,
			AQMode:              govpx.VP9AQCyclicRefresh,
			NoiseSensitivity:    1,
			Sharpness:           0,
			ErrorResilient:      true,
			TemporalScalability: govpx.TemporalScalabilityConfig{
				Enabled:                true,
				Mode:                   govpx.TemporalLayeringThreeLayers,
				LayerTargetBitrateKbps: cumulativeLayerSplit(layerTarget, temporalSplitPct),
			},
		}
	}

	return govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount:           spatialLayerCount,
		InterLayerPrediction: true,
		Layers:               layers,
	})
}

func splitBitrate(total int, splitPct [spatialLayerCount]int) [spatialLayerCount]int {
	var out [spatialLayerCount]int
	for i := 0; i < spatialLayerCount; i++ {
		v := total * splitPct[i] / 100
		if v < 50 {
			v = 50
		}
		out[i] = v
	}
	out[spatialLayerCount-1] = total
	return out
}

func cumulativeLayerSplit(total int, splitPct [3]int) [govpx.MaxTemporalLayers]int {
	var out [govpx.MaxTemporalLayers]int
	for i, pct := range splitPct {
		v := total * pct / 100
		if v < 10 {
			v = 10
		}
		out[i] = v
	}
	out[len(splitPct)-1] = total
	return out
}

func applyBitrate(svc *govpx.VP9SpatialSVCEncoder, totalKbps int) error {
	layerBitrates := splitBitrate(totalKbps, layerSplitPct)
	for i := 0; i < spatialLayerCount; i++ {
		layerTarget := layerBitrates[i]
		if err := svc.SetLayerRateControl(uint8(i), govpx.RateControlConfig{
			Mode:                govpx.RateControlCBR,
			TargetBitrateKbps:   layerTarget,
			MinQuantizer:        2,
			MaxQuantizer:        56,
			BufferSizeMs:        1000,
			BufferInitialSizeMs: 500,
			BufferOptimalSizeMs: 600,
		}); err != nil {
			return fmt.Errorf("layer %d: %w", i, err)
		}
	}
	return nil
}

func applyScreenMode(svc *govpx.VP9SpatialSVCEncoder, mode int) error {
	for i := 0; i < spatialLayerCount; i++ {
		if err := svc.SetLayerScreenContentMode(uint8(i), mode); err != nil {
			return fmt.Errorf("layer %d: %w", i, err)
		}
	}
	return nil
}

func forceKeyAll(svc *govpx.VP9SpatialSVCEncoder) {
	for i := 0; i < spatialLayerCount; i++ {
		layer, err := svc.LayerEncoder(uint8(i))
		if err != nil {
			return
		}
		layer.ForceKeyFrame()
	}
}

func pickCPUUsed(width, height int) int8 {
	// VP9 realtime cpu-used: 7 for HD, 8 for SD, mirroring libvpx defaults.
	if width*height >= 1280*720 {
		return 7
	}
	return 8
}

func pickThreads(width, height int) int {
	cpus := runtime.NumCPU()
	pixels := width * height
	switch {
	case pixels >= 1280*720 && cpus >= 4:
		return 4
	case pixels >= 640*360 && cpus >= 2:
		return 2
	default:
		return 1
	}
}

// statsTracker keeps a sliding window per (spatial, temporal) layer plus an
// access-unit aggregate so the browser can render kbps without having to track
// it client-side.
type statsTracker struct {
	mu       sync.Mutex
	windowed perLayerWindow
	au       window
	frames   int
	lastWall time.Time
	fpsEMA   float64
}

type window struct {
	bytes   int
	frames  int
	updated time.Time
}

type perLayerWindow [spatialLayerCount]struct {
	bytes      int
	since      time.Time
	lastKBPS   float64
	lastUpdate time.Time
}

func newStatsTracker() *statsTracker {
	now := time.Now()
	t := &statsTracker{lastWall: now}
	t.au.updated = now
	for i := range t.windowed {
		t.windowed[i].since = now
	}
	return t
}

func (t *statsTracker) observe(r govpx.VP9SpatialSVCEncodeResult, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.au.bytes += r.SizeBytes
	t.au.frames++
	if now.Sub(t.au.updated) > 500*time.Millisecond {
		t.au.updated = now
	}
	count := int(r.LayerCount)
	for i := 0; i < count && i < spatialLayerCount; i++ {
		t.windowed[i].bytes += r.Layers[i].SizeBytes
		if since := now.Sub(t.windowed[i].since); since >= time.Second {
			t.windowed[i].lastKBPS = float64(t.windowed[i].bytes*8) /
				since.Seconds() / 1000
			t.windowed[i].bytes = 0
			t.windowed[i].since = now
			t.windowed[i].lastUpdate = now
		}
	}
	t.frames++
	if dt := now.Sub(t.lastWall); dt > 0 {
		instant := 1.0 / dt.Seconds()
		if t.fpsEMA == 0 {
			t.fpsEMA = instant
		} else {
			t.fpsEMA = 0.9*t.fpsEMA + 0.1*instant
		}
		t.lastWall = now
	}
}

type telemetryLayer struct {
	SP      int     `json:"sp"`
	TP      int     `json:"tp"`
	Q       int     `json:"q"`
	Bytes   int     `json:"bytes"`
	KbpsR   float64 `json:"kbps_recent"`
	TL0     uint8   `json:"tl0"`
	Sync    bool    `json:"sync"`
	KF      bool    `json:"kf"`
	Dropped bool    `json:"dropped,omitempty"`
}

type telemetryTotals struct {
	Bytes int     `json:"bytes"`
	KbpsR float64 `json:"kbps_recent"`
	FPS   float64 `json:"fps"`
}

type telemetrySettings struct {
	TargetKbps int `json:"target_kbps"`
	ScreenMode int `json:"screen_mode"`
}

type telemetryMessage struct {
	Frame     int               `json:"frame"`
	TSMs      float64           `json:"ts_ms"`
	Layers    []telemetryLayer  `json:"layers"`
	Totals    telemetryTotals   `json:"totals"`
	Settings  telemetrySettings `json:"settings"`
	SSPresent bool              `json:"ss_present"`
}

func (t *statsTracker) snapshot(r govpx.VP9SpatialSVCEncodeResult, targetKbps, screenMode int, pts uint64) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	count := int(r.LayerCount)
	if count > spatialLayerCount {
		count = spatialLayerCount
	}
	layers := make([]telemetryLayer, count)
	for i := 0; i < count; i++ {
		l := r.Layers[i]
		layers[i] = telemetryLayer{
			SP:      int(l.SpatialLayerID),
			TP:      l.TemporalLayerID,
			Q:       l.Quantizer,
			Bytes:   l.SizeBytes,
			KbpsR:   t.windowed[i].lastKBPS,
			TL0:    l.TL0PICIDX,
			Sync:    l.TemporalLayerSync,
			KF:      l.KeyFrame,
			Dropped: l.Dropped,
		}
	}
	totalRecentKbps := 0.0
	for i := 0; i < count; i++ {
		totalRecentKbps += t.windowed[i].lastKBPS
	}
	msg := telemetryMessage{
		Frame:     t.frames,
		TSMs:      float64(pts) * 1000.0 / float64(rtpClockHz),
		Layers:    layers,
		Totals:    telemetryTotals{Bytes: r.SizeBytes, KbpsR: totalRecentKbps, FPS: t.fpsEMA},
		Settings:  telemetrySettings{TargetKbps: targetKbps, ScreenMode: screenMode},
		SSPresent: r.Layers[0].ScalabilityStructurePresent,
	}
	return json.Marshal(msg)
}

// drawScene paints one frame for every spatial layer. Each layer renders at
// its native resolution so the synthesised content scales naturally instead
// of being decimated, which keeps the encoder's per-layer search honest.
func drawScene(imgs []*image.YCbCr, t int) {
	for i, img := range imgs {
		drawFrameYCbCr(img, t, i)
	}
}

func drawFrameYCbCr(img *image.YCbCr, t int, layerIdx int) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	cx := (t * 6) % (w + 96)
	cy := h/2 - 40 + (t%24)*2
	const boxSize = 64

	// luma plane: scrolling rainbow gradient + bouncing box. Different layers
	// get slightly different phase so a viewer can tell which one was decoded
	// when SVC layer selection changes.
	phase := layerIdx * 17
	for y := 0; y < h; y++ {
		row := img.Y[y*img.YStride : y*img.YStride+w]
		base := byte(48 + ((y + t) & 0x3F))
		for x := 0; x < w; x++ {
			v := base + byte((x*3+t*2+phase)&0x7F)
			if x >= cx && x < cx+boxSize && y >= cy && y < cy+boxSize {
				v = 235
			}
			row[x] = v
		}
	}

	// chroma planes: smooth integer-cycled drift. Layer index seeds the U/V
	// offset so each spatial layer has a recognisable tint.
	uvW := (w + 1) / 2
	uvH := (h + 1) / 2
	uVal := byte(112 + 30*int(triWave(t+layerIdx*30, 120)))
	vVal := byte(140 + 30*int(triWave(t+60+layerIdx*30, 120)))
	for y := 0; y < uvH; y++ {
		uRow := img.Cb[y*img.CStride : y*img.CStride+uvW]
		vRow := img.Cr[y*img.CStride : y*img.CStride+uvW]
		for x := 0; x < uvW; x++ {
			uRow[x] = uVal
			vRow[x] = vVal
		}
	}
}

func triWave(t, period int) int8 {
	phase := ((t % period) * 4) / period
	switch phase {
	case 0, 1:
		return 1
	default:
		return -1
	}
}
