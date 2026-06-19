// govpx WebRTC VP9 SVC demo: synthesizes frames in Go, encodes a 3-layer
// spatial-SVC VP9 superframe with a 3-layer temporal pattern using govpx,
// streams the access units to the browser over WebRTC, and ships
// per-layer telemetry over a DataChannel so the page can render a live
// overlay. Bidirectional control messages on the same DataChannel let the
// page change bitrate, screen-content tuning, force keyframes, and pause
// the encoder. Run with `go run .` and open http://localhost:8080.
package main

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"github.com/thesyncim/govpx"
)

const (
	spatialLayerCount = 3
	temporalLayerMode = govpx.TemporalLayeringThreeLayers
	rtpClockHz        = 90000
	rtpPayloadMTU     = 1200 - 12
	vp9Profile0Fmtp   = "profile-id=0"
	iceGatherTimeout  = 10 * time.Second

	defaultFPS          = 30
	defaultBitrateKbps  = 800
	minLayerBitrateKbps = 1

	spatialCapBackoffOverruns       = 3
	spatialCapBackoffRecoveryFrames = 90
)

// layerDims holds the per-spatial-layer resolution, base to top. Each step
// is 2x in each dimension, satisfying VP9 SVC inter-layer scaling rules.
var layerDims = [spatialLayerCount][2]int{
	{160, 90},
	{320, 180},
	{640, 360},
}

// layerSplitPct holds the per-spatial-layer bitrate share, base to top.
// These are independent layer targets; libvpx sums them for total SVC budget.
var layerSplitPct = [spatialLayerCount]int{12, 36, 52}

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
    Three spatial layers (160x90, 320x180, 640x360) with a three-layer temporal pattern, packed
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
      <div class="layer" data-layer="0"><div class="res">160x90</div><dl></dl></div>
      <div class="layer" data-layer="1"><div class="res">320x180</div><dl></dl></div>
      <div class="layer" data-layer="2"><div class="res">640x360</div><dl></dl></div>
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
      <label>top layer</label>
      <button data-cap="1">160x90</button>
      <button data-cap="2">320x180</button>
      <button data-cap="3" class="on">640x360</button>
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
const ICE_GATHER_TIMEOUT_MS = 2000;
let latestRTCStats = null;

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
  if(latestRTCStats){
    row(totalsEl, "rx decoded", latestRTCStats.framesDecoded ?? "-");
    row(totalsEl, "rx dropped", latestRTCStats.framesDropped ?? "-");
    row(totalsEl, "rx lost", latestRTCStats.packetsLost ?? "-");
    row(totalsEl, "rx freezes", latestRTCStats.freezeCount ?? "-");
  }
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

for(const b of document.querySelectorAll("button[data-cap]")){
  b.onclick = () => {
    for(const o of document.querySelectorAll("button[data-cap]")) o.classList.remove("on");
    b.classList.add("on");
    sendCtl({type:"spatial", cap:+b.dataset.cap});
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
  window.govpxDemoPeerConnection = pc;
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
  const gathered = await waitForIceGatheringComplete(pc, ICE_GATHER_TIMEOUT_MS);
  if (!gathered) status.textContent = "ICE: gathering timeout, continuing";
  const res = await fetch("/offer", {
    method: "POST",
    headers: {"Content-Type":"application/json"},
    body: JSON.stringify(pc.localDescription),
  });
  if (!res.ok) { status.textContent = "offer failed: " + res.status; return; }
  await pc.setRemoteDescription(await res.json());
  updateRTCStats(pc).catch(() => {});
  setInterval(() => updateRTCStats(pc).catch(() => {}), 1000);
}

async function waitForIceGatheringComplete(pc, timeoutMs){
  if (pc.iceGatheringState === "complete") return true;
  return await new Promise(resolve => {
    let done = false;
    let timer = 0;
    const finish = ok => {
      if (done) return;
      done = true;
      clearTimeout(timer);
      pc.removeEventListener("icegatheringstatechange", onState);
      resolve(ok);
    };
    const onState = () => {
      if (pc.iceGatheringState === "complete") finish(true);
    };
    timer = setTimeout(() => finish(false), timeoutMs);
    pc.addEventListener("icegatheringstatechange", onState);
  });
}

async function updateRTCStats(pc){
  const report = await pc.getStats();
  let inbound = null;
  report.forEach(stat => {
    if(stat.type === "inbound-rtp" && (stat.kind === "video" || stat.mediaType === "video")){
      inbound = stat;
    }
  });
  if(!inbound) return;
  latestRTCStats = {
    packetsLost: statNumber(inbound.packetsLost),
    framesDecoded: statNumber(inbound.framesDecoded ?? inbound.framesReceived),
    framesDropped: statNumber(inbound.framesDropped),
    freezeCount: statNumber(inbound.freezeCount),
  };
}

function statNumber(value){
  return Number.isFinite(value) ? value : null;
}
start().catch(e => status.textContent = "error: " + e);
</script>
</body></html>`

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	fps := flag.Int("fps", defaultFPS, "encoded frame rate")
	bitrate := flag.Int("bitrate", defaultBitrateKbps, "total target bitrate in kbps")
	flag.Parse()
	if *fps <= 0 {
		log.Fatal("fps must be positive")
	}
	if *bitrate < spatialLayerCount*minLayerBitrateKbps {
		log.Fatalf("bitrate must be at least %d kbps",
			spatialLayerCount*minLayerBitrateKbps)
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
	spatialCap  atomic.Int32
	paused      atomic.Bool
	forceKey    atomic.Bool
}

type controlMessage struct {
	Type   string `json:"type"`
	Kbps   int    `json:"kbps,omitempty"`
	Mode   int    `json:"mode,omitempty"`
	Cap    int    `json:"cap,omitempty"`
	Paused bool   `json:"paused,omitempty"`
}

func handleOffer(w http.ResponseWriter, r *http.Request, cfg demoConfig) {
	handleOfferWithICEGatherWait(w, r, cfg, waitICEGatheringComplete)
}

func handleOfferWithICEGatherWait(
	w http.ResponseWriter,
	r *http.Request,
	cfg demoConfig,
	waitGather func(<-chan struct{}, time.Duration) bool,
) {
	var offer webrtc.SessionDescription
	if err := json.NewDecoder(r.Body).Decode(&offer); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !sdpOffersVP9Profile0Receive(offer.SDP) {
		http.Error(w, "VP9 profile 0 is required", http.StatusNotAcceptable)
		return
	}

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	track, err := webrtc.NewTrackLocalStaticRTP(
		vp9WebRTCCodecCapability(),
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
	ctl.spatialCap.Store(int32(spatialLayerCount))
	// The first access unit out of the SVC encoder is already a keyframe;
	// there's no need to seed forceKey here.

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
	if !waitGather(gather, iceGatherTimeout) {
		log.Printf("server ICE gathering timed out after %s; continuing with current answer",
			iceGatherTimeout)
	}
	local := pc.LocalDescription()
	if local == nil || !sdpAnswersVP9Profile0Send(local.SDP) {
		_ = pc.Close()
		http.Error(w, "VP9 profile 0 was not negotiated",
			http.StatusNotAcceptable)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	connected := make(chan struct{})
	var connectedOnce sync.Once
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		log.Printf("peer connection state: %s", s)
		if s == webrtc.PeerConnectionStateConnected {
			connectedOnce.Do(func() { close(connected) })
		}
		if peerConnectionStateIsTerminal(s) {
			cancel()
			_ = pc.Close()
		}
	})

	go drainRTCP(ctx, sender, ctl)
	go runEncoderAfterConnected(ctx, connected, track, telemetry, ctl, cfg)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(local)
}

func vp9WebRTCCodecCapability() webrtc.RTPCodecCapability {
	return webrtc.RTPCodecCapability{
		MimeType:    webrtc.MimeTypeVP9,
		ClockRate:   rtpClockHz,
		SDPFmtpLine: vp9Profile0Fmtp,
		RTCPFeedback: []webrtc.RTCPFeedback{
			{Type: "ccm", Parameter: "fir"},
			{Type: "nack", Parameter: "pli"},
		},
	}
}

func sdpNegotiatesVP9Profile0(sdp string) bool {
	return sdpHasVP9Profile0(sdp, sdpDirectionIsActive)
}

func sdpOffersVP9Profile0Receive(sdp string) bool {
	return sdpHasVP9Profile0(sdp, sdpDirectionAllowsReceive)
}

func sdpAnswersVP9Profile0Send(sdp string) bool {
	return sdpHasVP9Profile0(sdp, sdpDirectionAllowsSend)
}

func sdpHasVP9Profile0(sdp string, directionOK func(string) bool) bool {
	sessionDirection := "sendrecv"
	section := sdpMediaSection{direction: sessionDirection}
	haveSection := false
	for _, raw := range strings.Split(sdp, "\n") {
		line := strings.TrimSpace(strings.ToLower(raw))
		if strings.HasPrefix(line, "m=") {
			if haveSection && section.hasVP9Profile0(directionOK) {
				return true
			}
			media, active, payloadTypes := sdpMediaPayloadTypes(line)
			section = sdpMediaSection{
				media:                media,
				portActive:           active,
				payloadTypes:         payloadTypes,
				direction:            sessionDirection,
				vp9PayloadTypes:      make(map[string]bool),
				profile0PayloadTypes: make(map[string]bool),
			}
			haveSection = true
			continue
		}
		if direction, ok := sdpDirection(line); ok {
			if haveSection {
				section.direction = direction
			} else {
				sessionDirection = direction
			}
			continue
		}
		if !haveSection || !section.parsesVideoPayloadAttributes() {
			continue
		}
		switch {
		case strings.HasPrefix(line, "a=rtpmap:"):
			fields := strings.Fields(strings.TrimPrefix(line, "a=rtpmap:"))
			if len(fields) >= 2 && fields[1] == "vp9/90000" &&
				section.payloadTypes[fields[0]] {
				section.vp9PayloadTypes[fields[0]] = true
			}
		case strings.HasPrefix(line, "a=fmtp:"):
			fields := strings.Fields(strings.TrimPrefix(line, "a=fmtp:"))
			if len(fields) >= 2 && fmtpParamsContainVP9Profile0(
				strings.Join(fields[1:], " ")) &&
				section.payloadTypes[fields[0]] {
				section.profile0PayloadTypes[fields[0]] = true
			}
		}
	}
	return haveSection && section.hasVP9Profile0(directionOK)
}

type sdpMediaSection struct {
	media                string
	portActive           bool
	payloadTypes         map[string]bool
	direction            string
	vp9PayloadTypes      map[string]bool
	profile0PayloadTypes map[string]bool
}

func (s sdpMediaSection) parsesVideoPayloadAttributes() bool {
	return s.media == "video" && s.portActive
}

func (s sdpMediaSection) hasVP9Profile0(directionOK func(string) bool) bool {
	if !s.parsesVideoPayloadAttributes() || !directionOK(s.direction) {
		return false
	}
	for payloadType := range s.vp9PayloadTypes {
		if s.profile0PayloadTypes[payloadType] {
			return true
		}
	}
	return false
}

func sdpMediaPayloadTypes(line string) (string, bool, map[string]bool) {
	fields := strings.Fields(strings.TrimPrefix(line, "m="))
	if len(fields) < 4 {
		return "", false, nil
	}
	payloadTypes := make(map[string]bool, len(fields)-3)
	for _, payloadType := range fields[3:] {
		payloadTypes[payloadType] = true
	}
	return fields[0], !sdpMediaPortIsZero(fields[1]), payloadTypes
}

func sdpMediaPortIsZero(port string) bool {
	first, _, _ := strings.Cut(port, "/")
	first = strings.TrimLeft(first, "0")
	return first == ""
}

func sdpDirection(line string) (string, bool) {
	switch line {
	case "a=sendrecv", "a=sendonly", "a=recvonly", "a=inactive":
		return strings.TrimPrefix(line, "a="), true
	default:
		return "", false
	}
}

func sdpDirectionIsActive(direction string) bool {
	return direction != "inactive"
}

func sdpDirectionAllowsReceive(direction string) bool {
	return direction == "" || direction == "sendrecv" || direction == "recvonly"
}

func sdpDirectionAllowsSend(direction string) bool {
	return direction == "" || direction == "sendrecv" || direction == "sendonly"
}

func fmtpParamsContainVP9Profile0(params string) bool {
	for _, rawParam := range strings.Split(params, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(rawParam), "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == "profile-id" &&
			strings.TrimSpace(value) == "0" {
			return true
		}
	}
	return false
}

func peerConnectionStateIsTerminal(s webrtc.PeerConnectionState) bool {
	return s == webrtc.PeerConnectionStateClosed ||
		s == webrtc.PeerConnectionStateFailed
}

func waitICEGatheringComplete(done <-chan struct{}, timeout time.Duration) bool {
	if done == nil {
		return false
	}
	if timeout <= 0 {
		<-done
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

func applyControl(ctl *controlState, m controlMessage, cfg demoConfig) {
	switch m.Type {
	case "keyframe":
		ctl.forceKey.Store(true)
	case "pause":
		wasPaused := ctl.paused.Swap(m.Paused)
		if wasPaused && !m.Paused {
			ctl.forceKey.Store(true)
		}
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
	case "spatial":
		cap := clampSpatialCap(m.Cap)
		current := int(ctl.spatialCap.Swap(int32(cap)))
		if govpx.VP9WebRTCSpatialLayerChangeNeedsKeyFrame(current, cap) {
			// New cap level: reset references to the new effective top layer.
			ctl.forceKey.Store(true)
		}
	}
}

func clampSpatialCap(cap int) int {
	if cap < 1 {
		return 1
	}
	if cap > spatialLayerCount {
		return spatialLayerCount
	}
	return cap
}

func drainRTCP(ctx context.Context, sender *webrtc.RTPSender, ctl *controlState) {
	for {
		if ctx.Err() != nil {
			return
		}
		packets, _, err := sender.ReadRTCP()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) ||
				ctx.Err() != nil {
				return
			}
			log.Printf("rtcp read: %v", err)
			continue
		}
		if rtcpPacketsRequestKeyFrame(packets) {
			ctl.forceKey.Store(true)
		}
	}
}

func rtcpRequestsKeyFrame(raw []byte) bool {
	packets, err := rtcp.Unmarshal(raw)
	if err != nil {
		return false
	}
	return rtcpPacketsRequestKeyFrame(packets)
}

func rtcpPacketsRequestKeyFrame(packets []rtcp.Packet) bool {
	for _, packet := range packets {
		if rtcpPacketRequestsKeyFrame(packet) {
			return true
		}
	}
	return false
}

func rtcpPacketRequestsKeyFrame(packet rtcp.Packet) bool {
	switch p := packet.(type) {
	case *rtcp.PictureLossIndication:
		return true
	case *rtcp.FullIntraRequest:
		return len(p.FIR) > 0
	case *rtcp.CompoundPacket:
		if p == nil {
			return false
		}
		for _, child := range *p {
			if rtcpPacketRequestsKeyFrame(child) {
				return true
			}
		}
	}
	return false
}

func consumeForceKeyForActiveAccessUnit(ctl *controlState) (bool, bool) {
	if ctl.paused.Load() {
		return false, false
	}
	return true, ctl.forceKey.Swap(false)
}

func consumeForceKeyForWebRTCAccessUnit(
	ctl *controlState,
	packetizer *govpx.VP9WebRTCPacketizer,
) (bool, bool) {
	active, forceKey := consumeForceKeyForActiveAccessUnit(ctl)
	if !active {
		return false, false
	}
	if packetizer != nil && packetizer.NeedsKeyFrame() {
		forceKey = true
	}
	return active, forceKey
}

func requestKeyFrameAfterFailedAccessUnit(ctl *controlState) {
	ctl.forceKey.Store(true)
}

func spatialCapForAccessUnit(ctl *controlState, current int, forceKey bool) int {
	if !forceKey {
		return current
	}
	return clampSpatialCap(int(ctl.spatialCap.Load()))
}

type spatialCapBackoff struct {
	maxCap         int
	lastRequested  int
	overrunStreak  int
	recoveryStreak int
}

func newSpatialCapBackoff(initialCap int) spatialCapBackoff {
	cap := clampSpatialCap(initialCap)
	return spatialCapBackoff{
		maxCap:        cap,
		lastRequested: cap,
	}
}

func (b *spatialCapBackoff) effectiveCap(requestedCap int) int {
	requestedCap = clampSpatialCap(requestedCap)
	if b.maxCap == 0 {
		b.maxCap = requestedCap
	}
	if b.lastRequested == 0 {
		b.lastRequested = requestedCap
	}
	if requestedCap != b.lastRequested {
		b.lastRequested = requestedCap
		b.maxCap = requestedCap
		b.overrunStreak = 0
		b.recoveryStreak = 0
		return requestedCap
	}
	maxCap := clampSpatialCap(b.maxCap)
	if requestedCap < maxCap {
		return requestedCap
	}
	return maxCap
}

func (b *spatialCapBackoff) observe(
	activeCap int,
	requestedCap int,
	elapsed time.Duration,
	interval time.Duration,
) bool {
	activeCap = clampSpatialCap(activeCap)
	requestedCap = clampSpatialCap(requestedCap)
	if b.maxCap == 0 {
		b.maxCap = requestedCap
	}
	if b.lastRequested == 0 {
		b.lastRequested = requestedCap
	}
	if requestedCap != b.lastRequested {
		b.lastRequested = requestedCap
		b.maxCap = requestedCap
		b.overrunStreak = 0
		b.recoveryStreak = 0
		return activeCap != requestedCap
	}
	if interval <= 0 {
		return false
	}
	if elapsed > interval+interval/10 {
		b.recoveryStreak = 0
		if activeCap >= clampSpatialCap(b.maxCap) && b.maxCap > 1 {
			b.overrunStreak++
			if b.overrunStreak >= spatialCapBackoffOverruns {
				b.maxCap--
				b.overrunStreak = 0
				return activeCap != b.maxCap
			}
		} else {
			b.overrunStreak = 0
		}
		return false
	}
	b.overrunStreak = 0
	if requestedCap > b.maxCap && activeCap == b.maxCap &&
		elapsed < interval*3/4 {
		b.recoveryStreak++
		if b.recoveryStreak >= spatialCapBackoffRecoveryFrames {
			b.maxCap++
			b.recoveryStreak = 0
			return activeCap != b.maxCap
		}
	} else {
		b.recoveryStreak = 0
	}
	return false
}

func runEncoderAfterConnected(ctx context.Context, connected <-chan struct{},
	track *webrtc.TrackLocalStaticRTP, telemetry chan []byte, ctl *controlState,
	cfg demoConfig) {
	if !waitForPeerConnected(ctx, connected) {
		close(telemetry)
		return
	}
	ctl.forceKey.Store(true)
	runEncoder(ctx, track, telemetry, ctl, cfg)
}

func waitForPeerConnected(ctx context.Context, connected <-chan struct{}) bool {
	select {
	case <-ctx.Done():
		return false
	case <-connected:
		return true
	}
}

// runEncoder drives the spatial SVC encoder, packing one VP9 superframe per
// access unit into the track and sending a telemetry message describing every
// coded spatial/temporal layer to the browser.
func runEncoder(ctx context.Context, track *webrtc.TrackLocalStaticRTP,
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
	// VP9 keyframes at 640x360 fit well under this.
	packet := make([]byte, superframeBudget())
	var rtpFragments []govpx.RTPPayloadFragment
	var rtpPayloadBuf []byte
	interval := time.Second / time.Duration(cfg.FPS)
	rtpTimestampBase := randomUint32()
	rtpSequence := randomUint16()
	rtpPacketizer := govpx.NewVP9WebRTCPacketizer(randomUint16())

	startedAt := time.Now()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	currentBitrate := int(ctl.bitrateKbps.Load())
	currentScreen := int(ctl.screenMode.Load())
	currentSpatialCap := clampSpatialCap(int(ctl.spatialCap.Load()))
	capBackoff := newSpatialCapBackoff(currentSpatialCap)

	statsTracker := newStatsTracker()
	var lastMediaFrame uint64
	var haveMediaFrame bool
	for {
		var tickTime time.Time
		select {
		case <-ctx.Done():
			return
		case tickTime = <-ticker.C:
		}
		accessUnitStarted := time.Now()

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
		active, forceKey := consumeForceKeyForWebRTCAccessUnit(ctl,
			&rtpPacketizer)
		if !active {
			continue
		}
		requestedSpatialCap := clampSpatialCap(int(ctl.spatialCap.Load()))
		if forceKey {
			currentSpatialCap = capBackoff.effectiveCap(
				spatialCapForAccessUnit(ctl, currentSpatialCap, true))
		}
		if forceKey {
			forceKeyAll(svc)
		}

		mediaFrame := rtpMediaFrameForTick(startedAt, tickTime, cfg.FPS,
			lastMediaFrame, haveMediaFrame)
		lastMediaFrame = mediaFrame
		haveMediaFrame = true
		pts := rtpClockOffset(mediaFrame, cfg.FPS)
		rtpTimestamp := rtpTimestampBase + uint32(pts)
		sceneT := int(mediaFrame + 1)
		drawScene(imgs, sceneT)

		result, err := svc.EncodeActiveLayersIntoWithResult(imgs, packet,
			currentSpatialCap)
		if err != nil {
			log.Printf("EncodeActiveLayersIntoWithResult: %v (frame %d)", err,
				sceneT)
			requestKeyFrameAfterFailedAccessUnit(ctl)
			continue
		}

		rtpResult := result
		statsTracker.observe(rtpResult, time.Now())

		fragmentCount, payloadBytes, err := rtpPacketizer.
			SpatialSVCWebRTCPacketizationSize(rtpResult, rtpPayloadMTU)
		if err != nil {
			log.Printf("WebRTCRTPPacketizationSize: %v", err)
			requestKeyFrameAfterFailedAccessUnit(ctl)
			continue
		}
		if cap(rtpFragments) < fragmentCount {
			rtpFragments = make([]govpx.RTPPayloadFragment, fragmentCount)
		}
		rtpFragments = rtpFragments[:fragmentCount]
		if cap(rtpPayloadBuf) < payloadBytes {
			rtpPayloadBuf = make([]byte, payloadBytes)
		}
		rtpPayloadBuf = rtpPayloadBuf[:payloadBytes]
		fragmentCount, _, err = rtpPacketizer.PacketizeSpatialSVCWebRTCInto(
			rtpResult, rtpFragments, rtpPayloadBuf, rtpPayloadMTU)
		if err != nil {
			log.Printf("PacketizeWebRTCRTPInto: %v", err)
			requestKeyFrameAfterFailedAccessUnit(ctl)
			continue
		}
		for i := 0; i < fragmentCount; i++ {
			fragment := rtpFragments[i]
			pkt := rtp.Packet{
				Header: rtp.Header{
					Version:        2,
					Marker:         fragment.Marker,
					SequenceNumber: rtpSequence,
					Timestamp:      rtpTimestamp,
				},
				Payload: fragment.Payload,
			}
			rtpSequence++
			if err := track.WriteRTP(&pkt); err != nil {
				if !errors.Is(err, io.ErrClosedPipe) {
					log.Printf("WriteRTP: %v", err)
				}
				return
			}
		}

		if payload, err := statsTracker.snapshot(rtpResult, currentBitrate, currentScreen, pts); err == nil {
			pushTelemetry(telemetry, payload)
		}
		if capBackoff.observe(currentSpatialCap, requestedSpatialCap,
			time.Since(accessUnitStarted), interval) {
			ctl.forceKey.Store(true)
		}
	}
}

func randomUint32() uint32 {
	var b [4]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return uint32(time.Now().UnixNano())
	}
	return binary.BigEndian.Uint32(b[:])
}

func randomUint16() uint16 {
	var b [2]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return uint16(time.Now().UnixNano())
	}
	return binary.BigEndian.Uint16(b[:])
}

func rtpClockOffset(frame uint64, fps int) uint64 {
	if fps <= 0 {
		return 0
	}
	return frame * rtpClockHz / uint64(fps)
}

func rtpMediaFrameForTick(startedAt, tickTime time.Time, fps int,
	last uint64, haveLast bool,
) uint64 {
	frame := rtpScheduledFrameForTick(startedAt, tickTime, fps)
	if haveLast && frame <= last {
		return last + 1
	}
	return frame
}

func rtpScheduledFrameForTick(startedAt, tickTime time.Time, fps int) uint64 {
	if fps <= 0 || tickTime.Before(startedAt) {
		return 0
	}
	interval := time.Second / time.Duration(fps)
	if interval <= 0 {
		return 0
	}
	ticks := uint64(tickTime.Sub(startedAt) / interval)
	if ticks == 0 {
		return 0
	}
	return ticks - 1
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
	// Keep a 720p-class output buffer so forced keyframes and live bitrate
	// experiments have headroom while the demo defaults to a 640x360 top layer.
	const budgetLayerPixels = 1280 * 720
	return budgetLayerPixels/2 + spatialLayerCount*64*1024
}

func newSVCEncoder(cfg demoConfig) (*govpx.VP9SpatialSVCEncoder, error) {
	layerBitrates := splitBitrate(cfg.BitrateKbps, layerSplitPct)

	var layers [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions
	for i := 0; i < spatialLayerCount; i++ {
		w, h := layerDims[i][0], layerDims[i][1]
		layers[i] = newSVCLayerOptions(w, h, cfg.FPS, layerBitrates[i])
	}

	return govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount:           spatialLayerCount,
		InterLayerPrediction: true,
		Layers:               layers,
	})
}

func newSVCLayerOptions(width, height, fps, bitrateKbps int) govpx.VP9EncoderOptions {
	threads := pickThreads(width, height)
	return govpx.VP9EncoderOptions{
		Width:                    width,
		Height:                   height,
		FPS:                      fps,
		Threads:                  threads,
		RowMT:                    threads > 1,
		Deadline:                 govpx.DeadlineRealtime,
		CpuUsed:                  pickCPUUsed(width, height),
		RateControlModeSet:       true,
		RateControlMode:          govpx.RateControlCBR,
		TargetBitrateKbps:        bitrateKbps,
		TemporalScalability:      govpx.TemporalScalabilityConfig{Enabled: true, Mode: temporalLayerMode},
		ErrorResilient:           true,
		FrameParallelDecodingSet: true,
		FrameParallelDecoding:    true,
		MinQuantizer:             4,
		MaxQuantizer:             56,
		MaxKeyframeInterval:      128,
	}
}

func splitBitrate(total int, splitPct [spatialLayerCount]int) [spatialLayerCount]int {
	if total < spatialLayerCount*minLayerBitrateKbps {
		total = spatialLayerCount * minLayerBitrateKbps
	}
	var out [spatialLayerCount]int
	used := 0
	for i := 0; i < spatialLayerCount-1; i++ {
		v := total * splitPct[i] / 100
		if v < minLayerBitrateKbps {
			v = minLayerBitrateKbps
		}
		remainingLayers := spatialLayerCount - i - 1
		maxForLayer := total - used - remainingLayers*minLayerBitrateKbps
		if v > maxForLayer {
			v = maxForLayer
		}
		out[i] = v
		used += v
	}
	out[spatialLayerCount-1] = total - used
	return out
}

func applyBitrate(svc *govpx.VP9SpatialSVCEncoder, totalKbps int) error {
	layerBitrates := splitBitrate(totalKbps, layerSplitPct)
	for i := 0; i < spatialLayerCount; i++ {
		if err := svc.SetLayerBitrateKbps(uint8(i), layerBitrates[i]); err != nil {
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

// forceKeyAll asks the SVC encoder for a fresh key access unit after a browser
// PLI/FIR or manual keyframe request.
func forceKeyAll(svc *govpx.VP9SpatialSVCEncoder) {
	svc.ForceKeyFrame()
}

func pickCPUUsed(width, height int) int8 {
	_ = width * height
	return 8
}

func pickThreads(width, height int) int {
	cpus := runtime.NumCPU()
	maxTileCols := maxVP9TileColumns(width)
	if cpus < 2 || maxTileCols < 2 {
		return 1
	}
	if cpus >= 4 && maxTileCols >= 4 && width*height >= 640*360 {
		return 4
	}
	return 2
}

func maxVP9TileColumns(width int) int {
	miCols := (width + 7) >> 3
	sb64Cols := (miCols + 7) >> 3
	maxLog2 := 1
	for (sb64Cols >> uint(maxLog2)) >= 4 {
		maxLog2++
	}
	maxLog2--
	if maxLog2 <= 0 {
		return 1
	}
	cols := 1 << uint(maxLog2)
	if cols > 4 {
		return 4
	}
	return cols
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
	if count > spatialLayerCount {
		count = spatialLayerCount
	}
	for i := 0; i < count; i++ {
		t.windowed[i].bytes += r.Layers[i].SizeBytes
		if since := now.Sub(t.windowed[i].since); since >= time.Second {
			t.windowed[i].lastKBPS = float64(t.windowed[i].bytes*8) /
				since.Seconds() / 1000
			t.windowed[i].bytes = 0
			t.windowed[i].since = now
			t.windowed[i].lastUpdate = now
		}
	}
	for i := count; i < spatialLayerCount; i++ {
		t.windowed[i].bytes = 0
		t.windowed[i].since = now
		t.windowed[i].lastKBPS = 0
		t.windowed[i].lastUpdate = now
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
			TL0:     l.TL0PICIDX,
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
