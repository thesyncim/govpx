// govpx WebRTC VP8 super-demo: three simulcast VP8 encoders (320x180,
// 640x360, 1280x720), three temporal layers each, three independent
// WebRTC video tracks, and a live control surface for every libvpx VP8
// runtime knob this codec exposes. The browser shows all three
// renditions side-by-side, lets you steer per-rendition bitrate /
// denoiser / cyclic-refresh / screen-tuning / temporal layer cap, lets
// you paint a region-of-interest by clicking on any rendition, and
// surfaces an active-map toggle that masks the outer macroblock ring
// from inter coding. Telemetry and controls share one DataChannel.
// Run `go run .` then open http://localhost:8080.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
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
	renditionCount = 3
	rtpClockHz     = 90000
)

type renditionConfig struct {
	Name        string
	Width       int
	Height      int
	BitrateKbps int
}

var defaultRenditions = [renditionCount]renditionConfig{
	{Name: "low", Width: 320, Height: 180, BitrateKbps: 250},
	{Name: "mid", Width: 640, Height: 360, BitrateKbps: 800},
	{Name: "high", Width: 1280, Height: 720, BitrateKbps: 2000},
}

type demoConfig struct {
	Addr       string
	FPS        int
	Renditions [renditionCount]renditionConfig
}

var indexHTML = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>govpx VP8 super-demo</title>
<style>
:root{color-scheme:dark light;font-family:system-ui,sans-serif}
body{margin:0;padding:18px;max-width:1400px;margin-inline:auto;
  display:grid;grid-template-columns:minmax(0,1fr) 360px;gap:18px;align-items:start}
h1{margin:0 0 4px;font-size:22px}
h2{margin:14px 0 6px;font-size:13px;text-transform:uppercase;letter-spacing:.05em;opacity:.7}
p.lead{margin:0 0 10px;opacity:.75;font-size:13px;line-height:1.4}
.grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px;
  margin-bottom:10px}
.cell{background:rgba(127,127,127,.06);border-radius:8px;padding:8px;border:1px solid rgba(127,127,127,.18)}
.cell h3{margin:0 0 4px;font-size:13px;display:flex;justify-content:space-between;align-items:baseline}
.cell h3 small{font-weight:400;opacity:.65}
.cell video{width:100%;background:#111;border-radius:6px;display:block;cursor:crosshair}
.cell canvas.chart{width:100%;height:42px;display:block;margin-top:4px;
  background:rgba(127,127,127,.06);border-radius:4px}
.cell dl{display:grid;grid-template-columns:auto 1fr;gap:1px 8px;font-size:11px;
  margin:6px 0 0;font-variant-numeric:tabular-nums}
.cell dt{opacity:.6}
.cell dd{margin:0;text-align:right}
.cell.k{box-shadow:0 0 0 2px #ffcb6b inset}
.card{background:rgba(127,127,127,.08);border:1px solid rgba(127,127,127,.18);
  padding:12px 14px;border-radius:8px;margin-bottom:10px}
.card h2:first-child{margin-top:0}
.row{display:flex;gap:6px;flex-wrap:wrap;align-items:center;margin:6px 0}
.row label{font-size:12px;opacity:.7;min-width:74px}
.row input[type=range]{flex:1;min-width:120px}
.row span.val{font-variant-numeric:tabular-nums;min-width:80px;text-align:right;font-size:12px}
button,input[type=range]{font:inherit}
button{padding:5px 9px;border-radius:6px;border:1px solid rgba(127,127,127,.3);
  background:rgba(127,127,127,.1);cursor:pointer;font-size:12px}
button:hover{background:rgba(127,127,127,.2)}
button.on{outline:2px solid #82aaff}
#status{font-size:11px;opacity:.6;margin-top:4px;text-align:right}
.legend{font-size:11px;opacity:.7;margin-top:6px;line-height:1.5}
.legend kbd{background:rgba(127,127,127,.18);padding:1px 4px;border-radius:3px;
  font-family:monospace;font-size:10px}
.roi-overlay{position:relative}
.roi-overlay .marker{position:absolute;border:2px solid #ffcb6b;border-radius:50%;
  width:24px;height:24px;pointer-events:none;transform:translate(-50%,-50%)}
</style></head>
<body>
<main>
  <h1>govpx VP8 super-demo</h1>
  <p class="lead">
    Three simulcast VP8 encoders running in parallel with three temporal
    layers each, three WebRTC video tracks, one DataChannel control
    surface. Click on any video to drop a region-of-interest at that
    point and the encoder feeds it a high-quality segment.
  </p>
  <div class="grid">
    <div class="cell" data-rendition="0">
      <h3>low <small>320x180</small></h3>
      <div class="roi-overlay">
        <video autoplay playsinline muted></video>
      </div>
      <canvas class="chart"></canvas>
      <dl></dl>
    </div>
    <div class="cell" data-rendition="1">
      <h3>mid <small>640x360</small></h3>
      <div class="roi-overlay">
        <video autoplay playsinline muted></video>
      </div>
      <canvas class="chart"></canvas>
      <dl></dl>
    </div>
    <div class="cell" data-rendition="2">
      <h3>high <small>1280x720</small></h3>
      <div class="roi-overlay">
        <video autoplay playsinline muted></video>
      </div>
      <canvas class="chart"></canvas>
      <dl></dl>
    </div>
  </div>
  <div id="status">connecting&hellip;</div>
  <div class="legend">
    Click a video to set ROI. Buttons below steer each rendition's
    encoder independently. <kbd>0</kbd>/<kbd>1</kbd>/<kbd>2</kbd> on a
    cell focuses temporal cap to TL0/TL1/all.
  </div>
</main>
<aside>
  <div class="card">
    <h2>Global controls</h2>
    <div class="row">
      <label>active map</label>
      <button id="activeMap">edge mask off</button>
    </div>
    <div class="row">
      <label>force key</label>
      <button data-globalkf="0">low</button>
      <button data-globalkf="1">mid</button>
      <button data-globalkf="2">high</button>
      <button data-globalkf="-1">all</button>
    </div>
    <div class="row">
      <label>reset ROI</label>
      <button id="roiReset">clear all</button>
    </div>
  </div>
  <div class="card" data-controls="0">
    <h2>low &mdash; 320x180</h2>
  </div>
  <div class="card" data-controls="1">
    <h2>mid &mdash; 640x360</h2>
  </div>
  <div class="card" data-controls="2">
    <h2>high &mdash; 1280x720</h2>
  </div>
</aside>
<script>
const RENDITION_COUNT = 3;
const status = document.getElementById("status");
const cells = [];
const charts = [];
const samples = [[], [], []];
const MAX_SAMPLES = 200;

function row(dl, k, v){
  const dt = document.createElement("dt"); dt.textContent = k;
  const dd = document.createElement("dd"); dd.textContent = v;
  dl.appendChild(dt); dl.appendChild(dd);
}

function drawChart(idx){
  const c = charts[idx]; const ctx = c.getContext("2d");
  const w = c.width, h = c.height;
  ctx.clearRect(0,0,w,h);
  const s = samples[idx];
  if(!s.length) return;
  let mx = 1;
  for(const v of s) if(v > mx) mx = v;
  mx = Math.max(mx, 50);
  ctx.strokeStyle = "rgba(130,170,255,.85)";
  ctx.fillStyle = "rgba(130,170,255,.22)";
  ctx.lineWidth = 1.2;
  ctx.beginPath();
  for(let i=0;i<s.length;i++){
    const x = (i/(MAX_SAMPLES-1))*w;
    const y = h - (s[i]/mx)*(h-4) - 2;
    if(i===0) ctx.moveTo(x,y); else ctx.lineTo(x,y);
  }
  ctx.lineTo(w,h); ctx.lineTo(0,h); ctx.closePath();
  ctx.fill();
  ctx.beginPath();
  for(let i=0;i<s.length;i++){
    const x = (i/(MAX_SAMPLES-1))*w;
    const y = h - (s[i]/mx)*(h-4) - 2;
    if(i===0) ctx.moveTo(x,y); else ctx.lineTo(x,y);
  }
  ctx.stroke();
  ctx.fillStyle = "rgba(127,127,127,.7)";
  ctx.font = "9px system-ui";
  ctx.textAlign = "right";
  ctx.fillText(mx.toFixed(0)+" kbps", w-3, 10);
}

function renderRendition(idx, msg){
  const cell = cells[idx];
  const dl = cell.querySelector("dl");
  dl.textContent = "";
  cell.classList.toggle("k", !!msg.kf);
  // Reflect the *encoded* resolution in the cell heading so the user can
  // see resize requests take effect (the video element auto-scales and
  // hides this otherwise).
  if(msg.w && msg.h){
    const small = cell.querySelector("h3 small");
    if(small) small.textContent = msg.w + "x" + msg.h;
  }
  row(dl, "encoded", (msg.w||0) + "x" + (msg.h||0));
  row(dl, "q", msg.q);
  row(dl, "bytes", msg.bytes);
  row(dl, "kbps", (msg.kbps||0).toFixed(0));
  row(dl, "target", msg.target_kbps);
  if(typeof msg.encode_us === "number") {
    row(dl, "encode", (msg.encode_us/1000).toFixed(2) + " ms");
  }
  if(typeof msg.write_us === "number" && msg.write_us > 0) {
    row(dl, "write",  (msg.write_us/1000).toFixed(2) + " ms");
  }
  if(typeof msg.loop_us === "number" && msg.loop_us > 0) {
    const fps = msg.loop_us > 0 ? (1e6 / msg.loop_us).toFixed(1) : "-";
    row(dl, "loop",  (msg.loop_us/1000).toFixed(1) + " ms / " + fps + " fps");
  }
  row(dl, "T", msg.tp + (msg.sync?"↑":""));
  row(dl, "TL0", msg.tl0);
  if(msg.dropped) row(dl, "drop", "yes");
  if(msg.denoised) row(dl, "denoised", "yes");
  if(msg.scenecut) row(dl, "scenecut", "yes");
  samples[idx].push(msg.kbps||0);
  if(samples[idx].length > MAX_SAMPLES) samples[idx].shift();
  drawChart(idx);
}

let dc = null;
function sendCtl(obj){ if(dc && dc.readyState === "open") dc.send(JSON.stringify(obj)); }

// ---- per-rendition control surface ----
function buildControls(idx, target){
  const c = document.querySelector('[data-controls="'+idx+'"]');
  const mkRow = (label, body) => {
    const r = document.createElement("div"); r.className = "row";
    const l = document.createElement("label"); l.textContent = label;
    r.appendChild(l);
    r.appendChild(body);
    c.appendChild(r);
    return r;
  };
  // bitrate slider
  const br = document.createElement("input");
  br.type = "range"; br.min = 60; br.max = 5000; br.step = 50; br.value = target;
  const brLabel = document.createElement("span"); brLabel.className = "val";
  brLabel.textContent = target + " kbps";
  br.oninput = () => {
    brLabel.textContent = br.value + " kbps";
    sendCtl({type:"bitrate", id:idx, kbps:+br.value});
  };
  const brRow = mkRow("bitrate", br); brRow.appendChild(brLabel);

  // denoiser
  const denWrap = document.createElement("div"); denWrap.style.display="flex"; denWrap.style.gap="4px";
  ["off","low","med","high","high+","aggr"].forEach((label, k) => {
    const b = document.createElement("button");
    b.textContent = label;
    if(k===0) b.classList.add("on");
    b.onclick = () => {
      for(const o of denWrap.children) o.classList.remove("on");
      b.classList.add("on");
      sendCtl({type:"denoise", id:idx, level:k});
    };
    denWrap.appendChild(b);
  });
  mkRow("denoiser", denWrap);

  // screen / tuning
  const tuneWrap = document.createElement("div"); tuneWrap.style.display="flex"; tuneWrap.style.gap="4px";
  ["video","screen","film"].forEach((label, k) => {
    const b = document.createElement("button");
    b.textContent = label;
    if(k===0) b.classList.add("on");
    b.onclick = () => {
      for(const o of tuneWrap.children) o.classList.remove("on");
      b.classList.add("on");
      sendCtl({type:"screen", id:idx, mode:k});
    };
    tuneWrap.appendChild(b);
  });
  mkRow("tuning", tuneWrap);

  // resolution picker
  const RESOLUTIONS = [[160,90],[320,180],[480,272],[640,360],[864,480],[1280,720],[1920,1088]];
  const resWrap = document.createElement("div"); resWrap.style.display="flex"; resWrap.style.gap="4px"; resWrap.style.flexWrap="wrap";
  RESOLUTIONS.forEach(([w,h]) => {
    const b = document.createElement("button");
    b.textContent = w + "x" + h;
    b.dataset.w = w; b.dataset.h = h;
    b.onclick = () => {
      for(const o of resWrap.children) o.classList.remove("on");
      b.classList.add("on");
      sendCtl({type:"resize", id:idx, w, h});
      // also update the cell's title small text
      const cell = document.querySelector('[data-rendition="'+idx+'"]');
      if(cell){
        const small = cell.querySelector("h3 small");
        if(small) small.textContent = w + "x" + h;
      }
    };
    resWrap.appendChild(b);
  });
  // mark the current default
  resWrap.querySelectorAll("button").forEach(b => {
    if(+b.dataset.w === TARGET_DIMS[idx][0] && +b.dataset.h === TARGET_DIMS[idx][1]){
      b.classList.add("on");
    }
  });
  mkRow("size", resWrap);

  // ROI radius (in 16x16 macroblock cells)
  const roiR = document.createElement("input");
  roiR.type = "range"; roiR.min = 1; roiR.max = 20; roiR.step = 1; roiR.value = 2;
  const roiRLabel = document.createElement("span"); roiRLabel.className = "val";
  roiRLabel.textContent = "r=2 mb";
  roiR.oninput = () => {
    roiRLabel.textContent = "r=" + roiR.value + " mb";
    sendCtl({type:"roi-radius", id:idx, radius:+roiR.value});
  };
  const roiRow = mkRow("ROI radius", roiR); roiRow.appendChild(roiRLabel);

  // temporal cap
  const tWrap = document.createElement("div"); tWrap.style.display="flex"; tWrap.style.gap="4px";
  ["TL0","TL≤1","all"].forEach((label, k) => {
    const b = document.createElement("button");
    b.textContent = label;
    if(k===2) b.classList.add("on");
    b.onclick = () => {
      for(const o of tWrap.children) o.classList.remove("on");
      b.classList.add("on");
      sendCtl({type:"temporal", id:idx, cap:k});
    };
    tWrap.appendChild(b);
  });
  mkRow("temporal", tWrap);

  // misc
  const misc = document.createElement("div"); misc.style.display="flex"; misc.style.gap="4px";
  const kf = document.createElement("button"); kf.textContent = "force key";
  kf.onclick = () => sendCtl({type:"keyframe", id:idx});
  const pause = document.createElement("button"); pause.textContent = "pause"; let paused=false;
  pause.onclick = () => {
    paused = !paused;
    pause.classList.toggle("on", paused);
    pause.textContent = paused ? "resume" : "pause";
    sendCtl({type:"pause", id:idx, paused});
  };
  misc.appendChild(kf); misc.appendChild(pause);
  mkRow("actions", misc);
}

document.getElementById("activeMap").onclick = (e) => {
  const on = !e.target.classList.contains("on");
  e.target.classList.toggle("on", on);
  e.target.textContent = on ? "edge mask on" : "edge mask off";
  sendCtl({type:"activemap", enabled:on});
};
document.getElementById("roiReset").onclick = () => sendCtl({type:"roi-clear"});
for(const b of document.querySelectorAll("button[data-globalkf]")){
  b.onclick = () => sendCtl({type:"keyframe", id:+b.dataset.globalkf});
}

// ROI: click on a video → install ROI in the encoder at the corresponding
// macroblock cell. Display a marker so the user can see where they pointed.
function setupROI(idx){
  const cell = cells[idx];
  const wrap = cell.querySelector(".roi-overlay");
  const video = cell.querySelector("video");
  let marker = null;
  video.addEventListener("click", e => {
    const r = video.getBoundingClientRect();
    const u = Math.max(0, Math.min(1, (e.clientX - r.left)/r.width));
    const v = Math.max(0, Math.min(1, (e.clientY - r.top)/r.height));
    if(!marker){
      marker = document.createElement("div"); marker.className = "marker";
      wrap.appendChild(marker);
    }
    marker.style.left = (u*100)+"%";
    marker.style.top = (v*100)+"%";
    sendCtl({type:"roi", id:idx, u, v});
  });
}

async function start(){
  for(let i=0;i<RENDITION_COUNT;i++){
    cells[i] = document.querySelector('[data-rendition="'+i+'"]');
    charts[i] = cells[i].querySelector("canvas.chart");
    // resize chart for crisp rendering
    const r = charts[i].getBoundingClientRect();
    charts[i].width = Math.max(120, r.width|0);
    charts[i].height = Math.max(36, r.height|0);
    setupROI(i);
  }

  const pc = new RTCPeerConnection({iceServers:[{urls:"stun:stun.l.google.com:19302"}]});
  // Add three recvonly transceivers in low/mid/high order to mirror the
  // server-side AddTrack order.
  for(let i=0;i<RENDITION_COUNT;i++){
    pc.addTransceiver("video",{direction:"recvonly"});
  }
  let trackIdx = 0;
  pc.ontrack = e => {
    if(trackIdx < RENDITION_COUNT){
      const v = cells[trackIdx].querySelector("video");
      v.srcObject = e.streams[0] || new MediaStream([e.track]);
      trackIdx++;
    }
  };
  pc.oniceconnectionstatechange = () => { status.textContent = "ICE: " + pc.iceConnectionState; };
  pc.onconnectionstatechange = () => { status.textContent = "peer: " + pc.connectionState; };
  dc = pc.createDataChannel("demo", {ordered:true});
  dc.onopen = () => { status.textContent += " | dc open"; };
  dc.onmessage = ev => {
    try {
      const msg = JSON.parse(ev.data);
      if(typeof msg.target_kbps === "undefined" && msg.target){
        msg.target_kbps = msg.target;
      }
      if(typeof msg.id === "number") renderRendition(msg.id, msg);
    } catch(err){ console.error(err); }
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

const TARGETS = [` + targetsJSLiteral() + `];
const TARGET_DIMS = [` + dimsJSLiteral() + `];
for(let i=0;i<RENDITION_COUNT;i++) buildControls(i, TARGETS[i]);
start().catch(e => status.textContent = "error: " + e);
</script>
</body></html>`

func targetsJSLiteral() string {
	return fmt.Sprintf("%d,%d,%d",
		defaultRenditions[0].BitrateKbps,
		defaultRenditions[1].BitrateKbps,
		defaultRenditions[2].BitrateKbps)
}

func dimsJSLiteral() string {
	return fmt.Sprintf("[%d,%d],[%d,%d],[%d,%d]",
		defaultRenditions[0].Width, defaultRenditions[0].Height,
		defaultRenditions[1].Width, defaultRenditions[1].Height,
		defaultRenditions[2].Width, defaultRenditions[2].Height)
}

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	fps := flag.Int("fps", 30, "encoded frame rate")
	low := flag.Int("low-kbps", defaultRenditions[0].BitrateKbps, "low rendition kbps")
	mid := flag.Int("mid-kbps", defaultRenditions[1].BitrateKbps, "mid rendition kbps")
	high := flag.Int("high-kbps", defaultRenditions[2].BitrateKbps, "high rendition kbps")
	flag.Parse()
	if *fps <= 0 {
		log.Fatal("fps must be positive")
	}
	cfg := demoConfig{Addr: *addr, FPS: *fps, Renditions: defaultRenditions}
	cfg.Renditions[0].BitrateKbps = *low
	cfg.Renditions[1].BitrateKbps = *mid
	cfg.Renditions[2].BitrateKbps = *high

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The page is recompiled into the binary on every restart, so
		// the browser must always fetch a fresh copy to pick up new
		// runtime controls and per-rendition defaults. Without this a
		// soft refresh keeps stale JS and the user sees ghost
		// behaviour (e.g. wrong initial dimensions) that doesn't match
		// the running server.
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		_, _ = io.WriteString(w, indexHTML)
	})
	mux.HandleFunc("/offer", func(w http.ResponseWriter, r *http.Request) {
		handleOffer(w, r, cfg)
	})

	log.Printf("listening on http://localhost%s (3 simulcast VP8 @ %d/%d/%d kbps, %d fps)",
		cfg.Addr,
		cfg.Renditions[0].BitrateKbps,
		cfg.Renditions[1].BitrateKbps,
		cfg.Renditions[2].BitrateKbps,
		cfg.FPS)
	if err := http.ListenAndServe(cfg.Addr, mux); err != nil {
		log.Fatal(err)
	}
}

// renditionState owns one VP8 encoder's runtime knobs. atomic.* lets the
// DataChannel callback poke them while the encoder goroutine reads them
// between frames without a mutex.
type renditionState struct {
	cfg         renditionConfig
	bitrateKbps atomic.Int64
	screenMode  atomic.Int32
	denoise     atomic.Int32
	temporalCap atomic.Int32 // 0,1,2 == drop above TL0/TL1/none
	width       atomic.Int32 // 0 means leave alone; non-zero requests resize
	height      atomic.Int32
	paused      atomic.Bool
	forceKey    atomic.Bool

	roiRadius atomic.Int32 // in 16x16 macroblock cells; 0 == use default

	roiMu      sync.Mutex
	roiPending *govpx.ROIMap // installed on next encoder tick when non-nil
	roiActive  bool          // most recent install state for telemetry
}

type controlMessage struct {
	Type    string  `json:"type"`
	ID      int     `json:"id"`
	Kbps    int     `json:"kbps,omitempty"`
	Mode    int     `json:"mode,omitempty"`
	Level   int     `json:"level,omitempty"`
	Cap     int     `json:"cap,omitempty"`
	W       int     `json:"w,omitempty"`
	H       int     `json:"h,omitempty"`
	Radius  int     `json:"radius,omitempty"`
	U       float64 `json:"u,omitempty"`
	V       float64 `json:"v,omitempty"`
	Paused  bool    `json:"paused,omitempty"`
	Enabled bool    `json:"enabled,omitempty"`
}

type session struct {
	pc         *webrtc.PeerConnection
	tracks     [renditionCount]*webrtc.TrackLocalStaticSample
	renditions [renditionCount]*renditionState
	telemetry  chan []byte
	activeMap  atomic.Bool
	cfg        demoConfig
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

	sess := &session{
		pc:        pc,
		telemetry: make(chan []byte, 32),
		cfg:       cfg,
	}
	for i, rc := range cfg.Renditions {
		track, err := webrtc.NewTrackLocalStaticSample(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000},
			fmt.Sprintf("govpx-vp8-%s", rc.Name),
			fmt.Sprintf("govpx-%s", rc.Name),
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
		sess.tracks[i] = track
		rs := &renditionState{cfg: rc}
		rs.bitrateKbps.Store(int64(rc.BitrateKbps))
		rs.temporalCap.Store(2)
		sess.renditions[i] = rs
		// drain RTCP per rendition; PLI/FIR ⇒ force-key on that rendition.
		go drainRTCP(sender, rs)
	}

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
			applyControl(sess, m)
		})
		go func() {
			for payload := range sess.telemetry {
				if err := dc.SendText(string(payload)); err != nil {
					return
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

	go runRenditions(ctx, sess)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pc.LocalDescription())
}

func drainRTCP(sender *webrtc.RTPSender, rs *renditionState) {
	buf := make([]byte, 1500)
	for {
		_, _, err := sender.Read(buf)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("rtcp read: %v", err)
			}
			return
		}
		rs.forceKey.Store(true)
	}
}

func applyControl(sess *session, m controlMessage) {
	switch m.Type {
	case "keyframe":
		if m.ID < 0 {
			for _, rs := range sess.renditions {
				rs.forceKey.Store(true)
			}
			return
		}
		if rs := sess.rendition(m.ID); rs != nil {
			rs.forceKey.Store(true)
		}
	case "pause":
		if rs := sess.rendition(m.ID); rs != nil {
			rs.paused.Store(m.Paused)
		}
	case "screen":
		if rs := sess.rendition(m.ID); rs != nil {
			if m.Mode < 0 || m.Mode > 2 {
				return
			}
			rs.screenMode.Store(int32(m.Mode))
		}
	case "denoise":
		if rs := sess.rendition(m.ID); rs != nil {
			if m.Level < 0 || m.Level > 6 {
				return
			}
			rs.denoise.Store(int32(m.Level))
		}
	case "temporal":
		if rs := sess.rendition(m.ID); rs != nil {
			if m.Cap < 0 || m.Cap > 2 {
				return
			}
			if rs.temporalCap.Swap(int32(m.Cap)) != int32(m.Cap) {
				// Refresh the decoder's reference set on cap change so
				// suppressed temporal frames can't leave the receiver
				// stranded on a stale reference.
				rs.forceKey.Store(true)
			}
		}
	case "bitrate":
		if rs := sess.rendition(m.ID); rs != nil {
			kbps := m.Kbps
			if kbps < 50 {
				kbps = 50
			}
			if kbps > 20000 {
				kbps = 20000
			}
			rs.bitrateKbps.Store(int64(kbps))
		}
	case "resize":
		if rs := sess.rendition(m.ID); rs != nil {
			if !validDim(m.W) || !validDim(m.H) {
				return
			}
			rs.width.Store(int32(m.W))
			rs.height.Store(int32(m.H))
		}
	case "roi":
		if rs := sess.rendition(m.ID); rs != nil {
			rs.installROI(m.U, m.V)
		}
	case "roi-radius":
		if rs := sess.rendition(m.ID); rs != nil {
			r := m.Radius
			if r < 1 {
				r = 1
			}
			if r > 40 {
				r = 40
			}
			rs.roiRadius.Store(int32(r))
		}
	case "roi-clear":
		for _, rs := range sess.renditions {
			rs.installROI(-1, -1)
		}
	case "activemap":
		sess.activeMap.Store(m.Enabled)
	}
}

func (s *session) rendition(id int) *renditionState {
	if id < 0 || id >= renditionCount {
		return nil
	}
	return s.renditions[id]
}

// installROI computes a libvpx VP8 ROI map for this rendition with one
// boosted-quality segment centered at the normalised (u, v) coordinate.
// Passing u or v < 0 clears the map.
func (rs *renditionState) installROI(u, v float64) {
	rs.roiMu.Lock()
	defer rs.roiMu.Unlock()
	if u < 0 || v < 0 {
		rs.roiPending = &govpx.ROIMap{} // disabled marker
		rs.roiActive = false
		return
	}
	w, h := int(rs.width.Load()), int(rs.height.Load())
	if w == 0 || h == 0 {
		w, h = rs.cfg.Width, rs.cfg.Height
	}
	mbRows := (h + 15) >> 4
	mbCols := (w + 15) >> 4
	cellMap := make([]uint8, mbRows*mbCols)
	cy := int(v * float64(mbRows))
	cx := int(u * float64(mbCols))
	radius := int(rs.roiRadius.Load())
	if radius <= 0 {
		radius = 2
	}
	for r := 0; r < mbRows; r++ {
		dy := r - cy
		for c := 0; c < mbCols; c++ {
			dx := c - cx
			if dx*dx+dy*dy <= radius*radius {
				cellMap[r*mbCols+c] = 1 // boosted segment
			} else {
				cellMap[r*mbCols+c] = 0 // base
			}
		}
	}
	roi := &govpx.ROIMap{
		Enabled:   true,
		Rows:      mbRows,
		Cols:      mbCols,
		SegmentID: cellMap,
	}
	// Segment 1 gets a quality boost (negative delta = lower QP = higher q).
	roi.DeltaQuantizer[1] = -20
	roi.DeltaLoopFilter[1] = -10
	rs.roiPending = roi
	rs.roiActive = true
}

func runRenditions(ctx context.Context, sess *session) {
	defer close(sess.telemetry)
	var wg sync.WaitGroup
	for i := 0; i < renditionCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			runOneRendition(ctx, sess, idx)
		}(i)
	}
	wg.Wait()
}

func runOneRendition(ctx context.Context, sess *session, idx int) {
	rs := sess.renditions[idx]
	track := sess.tracks[idx]
	cfg := sess.cfg
	threads := pickThreads(rs.cfg.Width, rs.cfg.Height)

	enc, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               rs.cfg.Width,
		Height:              rs.cfg.Height,
		FPS:                 cfg.FPS,
		Threads:             threads,
		TokenPartitions:     pickTokenPartitions(threads),
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   rs.cfg.BitrateKbps,
		MinQuantizer:        2,
		MaxQuantizer:        56,
		UndershootPct:       100,
		OvershootPct:        15,
		BufferSizeMs:        1000,
		BufferInitialSizeMs: 500,
		BufferOptimalSizeMs: 600,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  30,
		MaxIntraBitratePct:  webrtcMaxIntraTargetPct(600, cfg.FPS),
		KeyFrameInterval:    3000,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             pickCPUUsed(rs.cfg.Width, rs.cfg.Height),
		StaticThreshold:     1,
		TemporalScalability: govpx.TemporalScalabilityConfig{
			Enabled: true,
			Mode:    govpx.TemporalLayeringThreeLayers,
		},
	})
	if err != nil {
		log.Printf("[%s] NewVP8Encoder: %v", rs.cfg.Name, err)
		return
	}
	defer enc.Close()

	img := newImage(rs.cfg.Width, rs.cfg.Height)
	packet := make([]byte, outputBufferSize(rs.cfg.Width, rs.cfg.Height))
	interval := time.Second / time.Duration(cfg.FPS)
	duration := uint64(rtpClockHz / cfg.FPS)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	tracker := newKbpsTracker()
	currentBitrate := rs.cfg.BitrateKbps
	currentScreen := 0
	currentDenoise := 0
	currentActiveMap := false
	currentWidth, currentHeight := rs.cfg.Width, rs.cfg.Height
	rs.width.Store(int32(currentWidth))
	rs.height.Store(int32(currentHeight))
	var pts uint64
	var sceneT int
	var lastTick time.Time
	var loopUS, writeUS int
	for {
		select {
		case <-ctx.Done():
			return
		case tick := <-ticker.C:
			if !lastTick.IsZero() {
				loopUS = int(tick.Sub(lastTick).Microseconds())
			}
			lastTick = tick
		}

		if wantW, wantH := int(rs.width.Load()), int(rs.height.Load()); wantW > 0 && wantH > 0 &&
			(wantW != currentWidth || wantH != currentHeight) {
			if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{
				Width:  wantW,
				Height: wantH,
			}); err != nil {
				log.Printf("[%s] SetRealtimeTarget(%dx%d): %v",
					rs.cfg.Name, wantW, wantH, err)
			} else {
				log.Printf("[%s] resize %dx%d -> %dx%d",
					rs.cfg.Name, currentWidth, currentHeight, wantW, wantH)
				img = newImage(wantW, wantH)
				if need := outputBufferSize(wantW, wantH); len(packet) < need {
					packet = make([]byte, need)
				}
				currentWidth, currentHeight = wantW, wantH
				// invalidate ROI/active maps for the new geometry; the
				// browser must re-arm them at the new mb dimensions.
				rs.roiMu.Lock()
				rs.roiActive = false
				rs.roiPending = nil
				rs.roiMu.Unlock()
				_ = enc.SetROIMap(nil)
				if currentActiveMap {
					_ = applyActiveMap(enc, wantW, wantH, true)
				}
			}
		}

		if want := int(rs.bitrateKbps.Load()); want != currentBitrate {
			if err := enc.SetBitrateKbps(want); err != nil {
				log.Printf("[%s] SetBitrateKbps(%d): %v", rs.cfg.Name, want, err)
			} else {
				currentBitrate = want
			}
		}
		if want := int(rs.screenMode.Load()); want != currentScreen {
			if err := enc.SetScreenContentMode(want); err != nil {
				log.Printf("[%s] SetScreenContentMode(%d): %v", rs.cfg.Name, want, err)
			} else {
				currentScreen = want
			}
		}
		if want := int(rs.denoise.Load()); want != currentDenoise {
			if err := enc.SetNoiseSensitivity(want); err != nil {
				log.Printf("[%s] SetNoiseSensitivity(%d): %v", rs.cfg.Name, want, err)
			} else {
				currentDenoise = want
			}
		}
		if want := sess.activeMap.Load(); want != currentActiveMap {
			if err := applyActiveMap(enc, currentWidth, currentHeight, want); err != nil {
				log.Printf("[%s] SetActiveMap: %v", rs.cfg.Name, err)
			} else {
				currentActiveMap = want
			}
		}
		// Drain pending ROI changes.
		rs.roiMu.Lock()
		roi := rs.roiPending
		rs.roiPending = nil
		rs.roiMu.Unlock()
		if roi != nil {
			if err := enc.SetROIMap(roi); err != nil {
				log.Printf("[%s] SetROIMap: %v", rs.cfg.Name, err)
			}
		}
		if rs.forceKey.Swap(false) {
			enc.ForceKeyFrame()
		}
		if rs.paused.Load() {
			continue
		}

		sceneT++
		drawFrame(img, sceneT, idx)

		flags := govpx.EncodeFlags(0)
		// Apply temporal cap by dropping non-base layers when the user
		// asked TL0 / TL1-only. The encoder still tracks the temporal
		// pattern; we just skip emission. Mirrors libvpx's
		// VP8E_SET_TEMPORAL_LAYER_ID-style cap.
		cap := int(rs.temporalCap.Load())
		encodeStart := time.Now()
		result, err := enc.EncodeInto(packet, img, pts, duration, flags)
		encodeUS := int(time.Since(encodeStart).Microseconds())
		pts += duration
		if err != nil {
			log.Printf("[%s] EncodeInto: %v", rs.cfg.Name, err)
			continue
		}
		if result.Dropped {
			pushTelemetry(sess.telemetry, dropTelemetry(idx, rs, currentBitrate, currentScreen, currentDenoise, currentWidth, currentHeight, encodeUS, loopUS))
			continue
		}
		// Only suppress emission for frames the encoder marked as
		// Droppable. Dropping a non-droppable frame poisons the
		// decoder's reference chain and causes the receiver to render
		// artifacts until the next keyframe.
		if cap < result.TemporalLayerID && result.Droppable {
			pushTelemetry(sess.telemetry, frameTelemetry(idx, rs, currentBitrate, currentScreen, currentDenoise, currentWidth, currentHeight, result, tracker, true, encodeUS, 0, loopUS))
			continue
		}

		writeStart := time.Now()
		if err := track.WriteSample(media.Sample{
			Data:     result.Data,
			Duration: interval,
		}); err != nil {
			if !errors.Is(err, io.ErrClosedPipe) {
				log.Printf("[%s] WriteSample: %v", rs.cfg.Name, err)
			}
			return
		}
		writeUS = int(time.Since(writeStart).Microseconds())
		tracker.observe(result.SizeBytes, time.Now())
		pushTelemetry(sess.telemetry, frameTelemetry(idx, rs, currentBitrate, currentScreen, currentDenoise, currentWidth, currentHeight, result, tracker, false, encodeUS, writeUS, loopUS))
	}
}

// applyActiveMap toggles a coarse edge mask: the outermost ring of
// macroblocks is marked inactive. On inter frames libvpx will ZEROMV-skip
// those blocks, which visibly freezes the edges and saves bits.
func applyActiveMap(enc *govpx.VP8Encoder, width, height int, enabled bool) error {
	if !enabled {
		return enc.SetActiveMap(nil, 0, 0)
	}
	rows := (height + 15) >> 4
	cols := (width + 15) >> 4
	m := make([]uint8, rows*cols)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			if r == 0 || c == 0 || r == rows-1 || c == cols-1 {
				m[r*cols+c] = 0 // inactive
			} else {
				m[r*cols+c] = 1 // active
			}
		}
	}
	return enc.SetActiveMap(m, rows, cols)
}

// kbpsTracker tracks bytes-per-second over a rolling 1-second window.
type kbpsTracker struct {
	bytes      int
	since      time.Time
	lastKbps   float64
	lastUpdate time.Time
}

func newKbpsTracker() *kbpsTracker {
	return &kbpsTracker{since: time.Now()}
}

func (t *kbpsTracker) observe(n int, now time.Time) {
	t.bytes += n
	if dt := now.Sub(t.since); dt >= time.Second {
		t.lastKbps = float64(t.bytes*8) / dt.Seconds() / 1000
		t.bytes = 0
		t.since = now
		t.lastUpdate = now
	}
}

func (t *kbpsTracker) kbps() float64 { return t.lastKbps }

type telemetryMessage struct {
	ID         int     `json:"id"`
	Frame      int     `json:"frame"`
	Width      int     `json:"w"`
	Height     int     `json:"h"`
	Q          int     `json:"q"`
	Bytes      int     `json:"bytes"`
	Kbps       float64 `json:"kbps"`
	TargetKbps int     `json:"target_kbps"`
	TP         int     `json:"tp"`
	TL0        uint8   `json:"tl0"`
	Sync       bool    `json:"sync"`
	KF         bool    `json:"kf"`
	Dropped    bool    `json:"dropped,omitempty"`
	Suppressed bool    `json:"suppressed,omitempty"`
	Denoised   bool    `json:"denoised,omitempty"`
	SceneCut   bool    `json:"scenecut,omitempty"`
	Screen     int     `json:"screen,omitempty"`
	Denoise    int     `json:"denoise,omitempty"`
	ROI        bool    `json:"roi,omitempty"`
	// EncodeUS is govpx's per-frame EncodeInto wall time in
	// microseconds. Exposed so the UI can plot per-frame encoder cost
	// alongside Q and bytes — useful for diagnosing bitrate-recovery
	// perception ("FPS doesn't recover after a low-bitrate dip"), where
	// the encode-time curve tracks Q and bytes, not the bitrate-change
	// event itself.
	EncodeUS int `json:"encode_us"`
	// WriteUS is the per-frame track.WriteSample wall time. A large
	// WriteUS with a small EncodeUS means the WebRTC RTP/pacer is the
	// bottleneck, not govpx encode.
	WriteUS int `json:"write_us"`
	// LoopUS is the tick-to-tick interval of the encoder loop in
	// microseconds. At 30fps the steady-state value is ~33333. If it
	// grows to 300000+ while EncodeUS stays small, the loop is being
	// throttled by something downstream of EncodeInto.
	LoopUS int `json:"loop_us"`
}

var frameCounter [renditionCount]int

func frameTelemetry(idx int, rs *renditionState, target, screen, denoise, width, height int,
	r govpx.EncodeResult, tracker *kbpsTracker, suppressed bool, encodeUS, writeUS, loopUS int,
) []byte {
	frameCounter[idx]++
	rs.roiMu.Lock()
	roi := rs.roiActive
	rs.roiMu.Unlock()
	msg := telemetryMessage{
		ID:         idx,
		Frame:      frameCounter[idx],
		Width:      width,
		Height:     height,
		Q:          r.Quantizer,
		Bytes:      r.SizeBytes,
		Kbps:       tracker.kbps(),
		TargetKbps: target,
		TP:         r.TemporalLayerID,
		TL0:        r.TL0PICIDX,
		Sync:       r.TemporalLayerSync,
		KF:         r.KeyFrame,
		Suppressed: suppressed,
		Denoised:   r.Denoised,
		SceneCut:   r.SceneCut,
		Screen:     screen,
		Denoise:    denoise,
		ROI:        roi,
		EncodeUS:   encodeUS,
		WriteUS:    writeUS,
		LoopUS:     loopUS,
	}
	out, _ := json.Marshal(msg)
	return out
}

func dropTelemetry(idx int, rs *renditionState, target, screen, denoise, width, height int, encodeUS, loopUS int) []byte {
	frameCounter[idx]++
	rs.roiMu.Lock()
	roi := rs.roiActive
	rs.roiMu.Unlock()
	msg := telemetryMessage{
		ID:         idx,
		Frame:      frameCounter[idx],
		Width:      width,
		Height:     height,
		TargetKbps: target,
		Screen:     screen,
		Denoise:    denoise,
		Dropped:    true,
		ROI:        roi,
		EncodeUS:   encodeUS,
		LoopUS:     loopUS,
	}
	out, _ := json.Marshal(msg)
	return out
}

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

// validDim accepts dimensions within the demo's supported range. Even
// values only (chroma subsampling needs both dimensions to be even);
// macroblock alignment is not required because the encoder pads
// internally. The lower bound matches the smallest preset offered by
// the browser UI (160x90).
func validDim(v int) bool {
	return v >= 32 && v <= 1920 && v%2 == 0
}

// pickCPUUsed mirrors libwebrtc's non-ARM VP8 complexity selection.
func pickCPUUsed(width, height int) int {
	if width*height < 352*288 {
		return -4
	}
	return -6
}

func pickThreads(width, height int) int {
	cores := max(runtime.NumCPU(), 1)
	pixels := width * height
	if pixels >= 1280*720 {
		return min(4, cores)
	}
	if pixels >= 640*360 {
		return min(2, cores)
	}
	return 1
}

func pickTokenPartitions(threads int) int {
	tokenPartitions := 0
	for partitions := 1; partitions < threads && tokenPartitions < 3; partitions <<= 1 {
		tokenPartitions++
	}
	return tokenPartitions
}

func webrtcMaxIntraTargetPct(optimalBufferMs, fps int) int {
	target := optimalBufferMs * fps / 20
	if target < 300 {
		return 300
	}
	return target
}

func outputBufferSize(width, height int) int {
	n := width*height*3/2 + 4096
	if n < 256*1024 {
		return 256 * 1024
	}
	return n
}

func newImage(w, h int) govpx.Image {
	// VP8 mode decision occasionally reads into the row immediately
	// after the visible bottom (the dot-artifact corner check at the
	// bottommost macroblock row addresses srcOff + (15+1)*stride+15),
	// so pad each plane to the macroblock-aligned height. The visible
	// rows beyond Height are never written by the painter and the
	// encoder doesn't care what's in them; only the buffer length
	// matters.
	paddedH := ((h + 15) >> 4) << 4
	uvW := (w + 1) / 2
	uvH := (h + 1) / 2
	paddedUVH := ((uvH + 15) >> 4) << 4
	return govpx.Image{
		Width:   w,
		Height:  h,
		Y:       make([]byte, w*paddedH),
		U:       make([]byte, uvW*paddedUVH),
		V:       make([]byte, uvW*paddedUVH),
		YStride: w,
		UStride: uvW,
		VStride: uvW,
	}
}

// drawFrame paints a slow scrolling gradient plus a bouncing bright
// square. Geometry is computed in normalised [0,1] coordinates so the
// scene stays visually continuous when the encoder is resized mid-stream
// (otherwise the same `t` would put the bouncing box near the top of a
// 160x90 frame and in the middle of a 1280x720 frame on the very next
// tick, producing a teleport that wastes bits on every resize). The
// per-rendition tint shifts so a viewer can tell which encoder produced
// the frame they're looking at.
func drawFrame(img govpx.Image, t int, idx int) {
	w, h := img.Width, img.Height
	// Normalised box center: bxN, byN in [0,1).
	bxN := float64((t*5)%600) / 600.0
	byN := 0.5 + 0.18*float64(int((t/2)%20)-10)/10.0
	boxN := 0.16 // box side as a fraction of width
	cx := int(bxN * float64(w))
	cy := int((byN - boxN/2) * float64(h))
	bw := int(boxN * float64(w))
	bh := int(boxN * float64(w))
	// gradient phase in normalised pixel units so the stripe scale tracks
	// the visible frame instead of producing a much finer pattern at low
	// resolution and a coarse one at high.
	phaseN := float64(idx) * 0.07
	for y := 0; y < h; y++ {
		row := img.Y[y*img.YStride : y*img.YStride+w]
		base := byte(48 + (((y*128)/h + t) & 0x3F))
		for x := 0; x < w; x++ {
			u := float64(x) / float64(w)
			v := base + byte((int(u*256.0)+t*2+int(phaseN*256))&0x7F)
			if x >= cx && x < cx+bw && y >= cy && y < cy+bh {
				v = 235
			}
			row[x] = v
		}
	}
	uvW := (w + 1) / 2
	uvH := (h + 1) / 2
	uVal := byte(112 + 30*int(triWave(t+idx*30, 120)))
	vVal := byte(140 + 30*int(triWave(t+60+idx*30, 120)))
	for y := 0; y < uvH; y++ {
		uRow := img.U[y*img.UStride : y*img.UStride+uvW]
		vRow := img.V[y*img.VStride : y*img.VStride+uvW]
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
