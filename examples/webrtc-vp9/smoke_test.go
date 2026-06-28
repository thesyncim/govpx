package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"

	"github.com/thesyncim/govpx"
)

// TestDemoEndToEnd boots the demo HTTP server, opens a pion peer that does
// the same offer/answer/DataChannel dance the browser does, and asserts the
// server delivers RTP packets and JSON telemetry within the per-frame
// budget the in-tree VP9 encoder can sustain. It covers the spatial-SVC
// encoder, the WebRTC track, the DataChannel control path, and the JSON
// schema the browser overlay expects.
func TestDemoEndToEnd(t *testing.T) {
	cfg := demoConfig{Addr: ":0", FPS: defaultFPS, BitrateKbps: defaultBitrateKbps}
	mux := http.NewServeMux()
	mux.HandleFunc("/offer", func(w http.ResponseWriter, r *http.Request) {
		handleOffer(w, r, cfg)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection: %v", err)
	}
	defer pc.Close()

	rtpCh := make(chan *rtp.Packet, 256)
	trackHeaderCh := make(chan rtpTrackHeaderForTest, 1)
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		select {
		case trackHeaderCh <- rtpTrackHeaderForTest{
			payloadType: uint8(track.PayloadType()),
			ssrc:        uint32(track.SSRC()),
		}:
		default:
		}
		go func() {
			for {
				packet, _, err := track.ReadRTP()
				if err != nil {
					return
				}
				if len(packet.Payload) > 0 {
					select {
					case rtpCh <- packet:
					default:
					}
				}
			}
		}()
	})

	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	); err != nil {
		t.Fatalf("AddTransceiverFromKind: %v", err)
	}

	dcMsgCh := make(chan []byte, 8)
	dc, err := pc.CreateDataChannel("demo", nil)
	if err != nil {
		t.Fatalf("CreateDataChannel: %v", err)
	}
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		select {
		case dcMsgCh <- append([]byte(nil), msg.Data...):
		default:
		}
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	gather := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription: %v", err)
	}
	<-gather

	body, err := json.Marshal(pc.LocalDescription())
	if err != nil {
		t.Fatalf("marshal offer: %v", err)
	}
	resp, err := http.Post(ts.URL+"/offer", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /offer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("offer status=%d body=%s", resp.StatusCode, raw)
	}
	var answer webrtc.SessionDescription
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	if !strings.Contains(strings.ToUpper(answer.SDP), "VP9") {
		t.Fatalf("answer SDP missing VP9:\n%s", answer.SDP)
	}
	if !strings.Contains(answer.SDP, vp9Profile0Fmtp) {
		t.Fatalf("answer SDP missing VP9 profile 0 fmtp:\n%s", answer.SDP)
	}
	for _, feedback := range []string{
		"goog-remb",
		"ccm fir",
		"nack",
		"nack pli",
		"transport-cc",
	} {
		if !sdpHasRTCPFeedbackForTest(answer.SDP, feedback) {
			t.Fatalf("answer SDP missing VP9 feedback %q:\n%s",
				feedback, answer.SDP)
		}
	}
	twccExtID := sdpExtmapIDForTest(t, answer.SDP, sdp.TransportCCURI)
	if err := pc.SetRemoteDescription(answer); err != nil {
		t.Fatalf("SetRemoteDescription: %v", err)
	}

	// Allow a generous window for the first access unit to land. The point is
	// to prove the wire works end-to-end, not to gate on local scheduler noise.
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	trackHeader := readRTPTrackHeaderForTest(t, ctx, trackHeaderCh)
	firstAU := readVP9RTPAccessUnitForTest(t, ctx, rtpCh)
	assertRTPAccessUnitHeaderMatchesTrackForTest(t, "first RTP access unit",
		firstAU, trackHeader)
	assertRTPAccessUnitHasHeaderExtensionForTest(t, "first RTP access unit",
		firstAU, twccExtID)
	firstDesc := assertWebRTCRTPAccessUnitForTest(t, firstAU,
		spatialLayerCount, true)
	secondAU := readVP9RTPAccessUnitForTest(t, ctx, rtpCh)
	assertRTPAccessUnitHeaderMatchesTrackForTest(t, "second RTP access unit",
		secondAU, trackHeader)
	assertRTPAccessUnitHasHeaderExtensionForTest(t, "second RTP access unit",
		secondAU, twccExtID)
	secondDesc := assertWebRTCRTPAccessUnitForTest(t, secondAU,
		spatialLayerCount, false)
	if got, want := secondAU[0].SequenceNumber, firstAU[0].SequenceNumber+uint16(len(firstAU)); got != want {
		t.Fatalf("second RTP access unit first sequence = %d, want %d",
			got, want)
	}
	assertRTPMediaTimestampAdvancedForTest(t, "second RTP access unit",
		firstAU[0].Timestamp, secondAU[0].Timestamp, defaultFPS)
	if got, want := secondDesc.PictureID, govpx.NextVP9RTPPictureID(firstDesc.PictureID); got != want {
		t.Fatalf("second RTP picture ID = %d, want %d", got, want)
	}
	if err := pc.WriteRTCP([]rtcp.Packet{
		&rtcp.PictureLossIndication{MediaSSRC: firstAU[0].SSRC},
	}); err != nil {
		t.Fatalf("send PLI: %v", err)
	}
	pliAU, pliDesc := readVP9RTPKeyAccessUnitAfterFeedbackForTest(t, ctx, rtpCh,
		secondAU, secondDesc)
	assertRTPAccessUnitHeaderMatchesTrackForTest(t, "PLI RTP access unit",
		pliAU, trackHeader)
	if pliDesc.InterPicturePredicted {
		t.Fatal("PLI response base descriptor kept inter-picture prediction")
	}
	if !pliDesc.ScalabilityStructurePresent {
		t.Fatal("PLI response did not carry key access-unit scalability structure")
	}
	if err := dc.SendText(`{"type":"spatial","cap":1}`); err != nil {
		t.Fatalf("send spatial cap ctl: %v", err)
	}
	capDesc := readVP9RTPAccessUnitWithActiveSpatialLayersForTest(t, ctx,
		rtpCh, pliAU, pliDesc, 1)
	if capDesc.InterPicturePredicted {
		t.Fatal("spatial-cap response base descriptor kept inter-picture prediction")
	}
	if !capDesc.ScalabilityStructurePresent ||
		capDesc.ScalabilityStructure.SpatialLayerCount != 1 {
		t.Fatalf("spatial-cap response SS = present:%t layers:%d, want active base-only key",
			capDesc.ScalabilityStructurePresent,
			capDesc.ScalabilityStructure.SpatialLayerCount)
	}
	if capDesc.ScalabilityStructure.Width[1] != 0 ||
		capDesc.ScalabilityStructure.Height[1] != 0 {
		t.Fatalf("spatial-cap response leaked hidden dimensions = %dx%d",
			capDesc.ScalabilityStructure.Width[1],
			capDesc.ScalabilityStructure.Height[1])
	}

	desc := firstDesc
	if !desc.LayerIndicesPresent || desc.SpatialID != 0 {
		t.Fatalf("first RTP descriptor layer metadata = present:%v sid:%d, want base spatial layer metadata",
			desc.LayerIndicesPresent, desc.SpatialID)
	}
	if !desc.PictureIDPresent || !desc.PictureID15Bit {
		t.Fatalf("first RTP picture ID = present:%v 15bit:%v, want 15-bit PictureID",
			desc.PictureIDPresent, desc.PictureID15Bit)
	}
	if !desc.ScalabilityStructurePresent ||
		desc.ScalabilityStructure.SpatialLayerCount != spatialLayerCount {
		t.Fatalf("first RTP scalability structure = present:%v layers:%d, want %d-layer SVC",
			desc.ScalabilityStructurePresent,
			desc.ScalabilityStructure.SpatialLayerCount, spatialLayerCount)
	}
	if desc.FlexibleMode {
		t.Fatal("first RTP descriptor used flexible VP9 mode")
	}
	if !desc.ScalabilityStructure.PictureGroupPresent ||
		len(desc.ScalabilityStructure.PictureGroups) == 0 {
		t.Fatalf("first RTP non-flexible SS temporal groups = present:%v count:%d, want GOF",
			desc.ScalabilityStructure.PictureGroupPresent,
			len(desc.ScalabilityStructure.PictureGroups))
	}

	var raw []byte
	select {
	case raw = <-dcMsgCh:
	case <-ctx.Done():
		t.Fatalf("no DataChannel telemetry received within timeout")
	}

	var msg telemetryMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("telemetry json: %v\npayload=%s", err, raw)
	}
	if len(msg.Layers) != spatialLayerCount {
		t.Fatalf("telemetry layer count = %d, want %d (msg=%s)",
			len(msg.Layers), spatialLayerCount, raw)
	}
	if msg.Settings.TargetKbps != cfg.BitrateKbps {
		t.Fatalf("telemetry target kbps = %d, want %d",
			msg.Settings.TargetKbps, cfg.BitrateKbps)
	}
	if msg.Settings.ActiveSpatialLayers != len(msg.Layers) {
		t.Fatalf("telemetry active spatial layers = %d, want %d",
			msg.Settings.ActiveSpatialLayers, len(msg.Layers))
	}
	if msg.Settings.RequestedSpatialLayers < msg.Settings.ActiveSpatialLayers ||
		msg.Settings.RequestedSpatialLayers > spatialLayerCount {
		t.Fatalf("telemetry requested spatial layers = %d, active = %d",
			msg.Settings.RequestedSpatialLayers,
			msg.Settings.ActiveSpatialLayers)
	}
	if msg.Sender.EncodeMs <= 0 ||
		msg.Sender.PacketizeMs < 0 ||
		msg.Sender.WriteMs < 0 ||
		msg.Sender.AccessUnitMs < msg.Sender.EncodeMs ||
		msg.Sender.RTPPackets <= 0 {
		t.Fatalf("sender telemetry = %+v, want live encode/write timings and RTP packet count",
			msg.Sender)
	}
	for i, layer := range msg.Layers {
		if layer.SP != i {
			t.Fatalf("layer %d SP = %d", i, layer.SP)
		}
		if layer.TP < 0 || layer.TP >= 3 {
			t.Fatalf("layer %d TP = %d, want three-layer temporal id", i, layer.TP)
		}
	}

	// Round-trip a control message; we don't gate on it reaching telemetry
	// because at the encoder's current pace the assertion window is wider
	// than the test budget.
	if err := dc.SendText(`{"type":"keyframe"}`); err != nil {
		t.Fatalf("send keyframe ctl: %v", err)
	}
}

type rtpTrackHeaderForTest struct {
	payloadType uint8
	ssrc        uint32
}

func readRTPTrackHeaderForTest(
	t *testing.T,
	ctx context.Context,
	ch <-chan rtpTrackHeaderForTest,
) rtpTrackHeaderForTest {
	t.Helper()
	select {
	case header := <-ch:
		if header.payloadType == 0 {
			t.Fatal("track RTP payload type was zero")
		}
		if header.ssrc == 0 {
			t.Fatal("track RTP SSRC was zero")
		}
		return header
	case <-ctx.Done():
		t.Fatalf("no RTP track metadata received within timeout")
		return rtpTrackHeaderForTest{}
	}
}

func assertRTPAccessUnitHeaderMatchesTrackForTest(
	t *testing.T,
	label string,
	packets []*rtp.Packet,
	trackHeader rtpTrackHeaderForTest,
) {
	t.Helper()
	if len(packets) == 0 {
		t.Fatalf("%s was empty", label)
	}
	for i, packet := range packets {
		if packet.PayloadType != trackHeader.payloadType {
			t.Fatalf("%s packet %d payload type = %d, want negotiated VP9 payload type %d",
				label, i, packet.PayloadType, trackHeader.payloadType)
		}
		if packet.SSRC != trackHeader.ssrc {
			t.Fatalf("%s packet %d SSRC = %d, want negotiated SSRC %d",
				label, i, packet.SSRC, trackHeader.ssrc)
		}
	}
}

func TestHandleOfferRejectsOfferWithoutVP9Profile0(t *testing.T) {
	cfg := demoConfig{Addr: ":0", FPS: defaultFPS, BitrateKbps: defaultBitrateKbps}
	mux := http.NewServeMux()
	mux.HandleFunc("/offer", func(w http.ResponseWriter, r *http.Request) {
		handleOffer(w, r, cfg)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection: %v", err)
	}
	defer pc.Close()
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	); err != nil {
		t.Fatalf("AddTransceiverFromKind: %v", err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if !offerSupportsDemoVP9(offer.SDP, cfg) {
		t.Fatalf("test offer unexpectedly missing VP9 profile 0:\n%s", offer.SDP)
	}
	offer.SDP = strings.ReplaceAll(offer.SDP, vp9Profile0Fmtp, "profile-id=2")
	if offerSupportsDemoVP9(offer.SDP, cfg) {
		t.Fatalf("mutated test offer still negotiates VP9 profile 0:\n%s", offer.SDP)
	}

	body, err := json.Marshal(offer)
	if err != nil {
		t.Fatalf("marshal offer: %v", err)
	}
	resp, err := http.Post(ts.URL+"/offer", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /offer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotAcceptable {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("offer status=%d body=%s, want %d",
			resp.StatusCode, raw, http.StatusNotAcceptable)
	}
}

func TestHandleOfferRejectsOfferWithVP9ReceiverCapsBelowDemoLayer(t *testing.T) {
	cfg := demoConfig{Addr: ":0", FPS: defaultFPS, BitrateKbps: defaultBitrateKbps}
	mux := http.NewServeMux()
	mux.HandleFunc("/offer", func(w http.ResponseWriter, r *http.Request) {
		handleOffer(w, r, cfg)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection: %v", err)
	}
	defer pc.Close()
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	); err != nil {
		t.Fatalf("AddTransceiverFromKind: %v", err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if !offerSupportsDemoVP9(offer.SDP, cfg) {
		t.Fatalf("test offer unexpectedly missing VP9 profile 0:\n%s", offer.SDP)
	}
	offer.SDP = strings.ReplaceAll(offer.SDP, vp9Profile0Fmtp,
		"profile-id=0; max-fr=30; max-fs=919")
	if offerSupportsDemoVP9(offer.SDP, cfg) {
		t.Fatalf("mutated test offer still allows demo top layer:\n%s", offer.SDP)
	}

	body, err := json.Marshal(offer)
	if err != nil {
		t.Fatalf("marshal offer: %v", err)
	}
	resp, err := http.Post(ts.URL+"/offer", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /offer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotAcceptable {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("offer status=%d body=%s, want %d",
			resp.StatusCode, raw, http.StatusNotAcceptable)
	}
}

func TestPlainVP9ModeUsesPlainReceiverCaps(t *testing.T) {
	svcCfg := demoConfig{Addr: ":0", FPS: defaultFPS, BitrateKbps: defaultBitrateKbps}
	plainCfg := svcCfg
	plainCfg.PlainVP9Mode = true

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection: %v", err)
	}
	defer pc.Close()
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	); err != nil {
		t.Fatalf("AddTransceiverFromKind: %v", err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	offer.SDP = strings.ReplaceAll(offer.SDP, vp9Profile0Fmtp,
		"profile-id=0; max-fr=30; max-fs=240")
	if offerSupportsDemoVP9(offer.SDP, svcCfg) {
		t.Fatalf("SVC mode accepted caps below the top spatial layer:\n%s",
			offer.SDP)
	}
	if !offerSupportsDemoVP9(offer.SDP, plainCfg) {
		t.Fatalf("plain VP9 mode rejected caps sufficient for %dx%d:\n%s",
			plainVP9Width, plainVP9Height, offer.SDP)
	}
}

func TestHandleOfferContinuesAfterServerICEGatherTimeout(t *testing.T) {
	cfg := demoConfig{Addr: ":0", FPS: defaultFPS, BitrateKbps: defaultBitrateKbps}
	waitCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/offer", func(w http.ResponseWriter, r *http.Request) {
		handleOfferWithICEGatherWait(w, r, cfg,
			func(done <-chan struct{}, timeout time.Duration) bool {
				waitCalled = true
				if done == nil {
					t.Fatal("server ICE gather wait received nil channel")
				}
				if timeout != iceGatherTimeout {
					t.Fatalf("server ICE gather timeout = %s, want %s",
						timeout, iceGatherTimeout)
				}
				return false
			})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection: %v", err)
	}
	defer pc.Close()
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	); err != nil {
		t.Fatalf("AddTransceiverFromKind: %v", err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	gather := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription: %v", err)
	}
	<-gather

	body, err := json.Marshal(pc.LocalDescription())
	if err != nil {
		t.Fatalf("marshal offer: %v", err)
	}
	resp, err := http.Post(ts.URL+"/offer", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /offer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("offer status=%d body=%s, want %d",
			resp.StatusCode, raw, http.StatusOK)
	}
	if !waitCalled {
		t.Fatal("server ICE gather wait was not called")
	}
	var answer webrtc.SessionDescription
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	if !govpx.VP9SDPAnswersProfile0Send(answer.SDP) {
		t.Fatalf("answer after forced ICE timeout does not send VP9 profile 0:\n%s",
			answer.SDP)
	}
	if err := pc.SetRemoteDescription(answer); err != nil {
		t.Fatalf("SetRemoteDescription(answer): %v", err)
	}
}

func TestHandleOfferRejectsOfferThatCannotReceiveVP9(t *testing.T) {
	cfg := demoConfig{Addr: ":0", FPS: defaultFPS, BitrateKbps: defaultBitrateKbps}
	mux := http.NewServeMux()
	mux.HandleFunc("/offer", func(w http.ResponseWriter, r *http.Request) {
		handleOffer(w, r, cfg)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection: %v", err)
	}
	defer pc.Close()
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	); err != nil {
		t.Fatalf("AddTransceiverFromKind: %v", err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if !offerSupportsDemoVP9(offer.SDP, cfg) {
		t.Fatalf("test offer unexpectedly missing receivable VP9 profile 0:\n%s",
			offer.SDP)
	}
	offer.SDP = strings.ReplaceAll(offer.SDP, "a=recvonly", "a=sendonly")
	if offerSupportsDemoVP9(offer.SDP, cfg) {
		t.Fatalf("sendonly test offer still allows VP9 receive:\n%s", offer.SDP)
	}
	body, err := json.Marshal(offer)
	if err != nil {
		t.Fatalf("marshal offer: %v", err)
	}
	resp, err := http.Post(ts.URL+"/offer", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /offer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotAcceptable {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("offer status=%d body=%s, want %d",
			resp.StatusCode, raw, http.StatusNotAcceptable)
	}
}

func readVP9RTPAccessUnitForTest(
	t *testing.T,
	ctx context.Context,
	rtpCh <-chan *rtp.Packet,
) []*rtp.Packet {
	t.Helper()
	var out []*rtp.Packet
	for {
		select {
		case packet := <-rtpCh:
			if packet == nil || len(packet.Payload) == 0 {
				continue
			}
			desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(packet.Payload)
			if err != nil {
				t.Fatalf("ParseVP9RTPPayloadDescriptor while reading AU: %v", err)
			}
			if len(out) == 0 &&
				(!desc.LayerIndicesPresent || desc.SpatialID != 0 || !desc.StartOfFrame) {
				continue
			}
			copyPacket := *packet
			copyPacket.Payload = append([]byte(nil), packet.Payload...)
			out = append(out, &copyPacket)
			if packet.Marker {
				return out
			}
		case <-ctx.Done():
			t.Fatalf("no complete RTP access unit received within timeout")
		}
	}
}

func readVP9RTPKeyAccessUnitAfterFeedbackForTest(
	t *testing.T,
	ctx context.Context,
	rtpCh <-chan *rtp.Packet,
	prevAU []*rtp.Packet,
	prevDesc govpx.VP9RTPPayloadDescriptor,
) ([]*rtp.Packet, govpx.VP9RTPPayloadDescriptor) {
	t.Helper()
	if len(prevAU) == 0 {
		t.Fatal("feedback wait started without previous RTP access unit")
	}
	const maxAccessUnits = defaultFPS
	for attempt := 0; attempt < maxAccessUnits; attempt++ {
		au := readVP9RTPAccessUnitForTest(t, ctx, rtpCh)
		activeLayers := rtpAccessUnitSpatialLayerCountForTest(t, au)
		first, _, err := govpx.ParseVP9RTPPayloadDescriptor(au[0].Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor feedback AU: %v", err)
		}
		desc := assertWebRTCRTPAccessUnitForTest(t, au, activeLayers,
			first.ScalabilityStructurePresent)
		prevLastSeq := prevAU[0].SequenceNumber + uint16(len(prevAU)-1)
		if got, want := au[0].SequenceNumber, prevLastSeq+1; got != want {
			t.Fatalf("feedback RTP access unit first sequence = %d, want %d",
				got, want)
		}
		assertRTPMediaTimestampAdvancedForTest(t, "feedback RTP access unit",
			prevAU[0].Timestamp, au[0].Timestamp, maxAccessUnits)
		if got, want := desc.PictureID, govpx.NextVP9RTPPictureID(prevDesc.PictureID); got != want {
			t.Fatalf("feedback RTP picture ID = %d, want %d", got, want)
		}
		if desc.ScalabilityStructurePresent {
			return au, desc
		}
		prevAU = au
		prevDesc = desc
	}
	t.Fatalf("receiver feedback did not produce a key RTP access unit within %d frames",
		maxAccessUnits)
	return nil, govpx.VP9RTPPayloadDescriptor{}
}

func readVP9RTPAccessUnitWithActiveSpatialLayersForTest(
	t *testing.T,
	ctx context.Context,
	rtpCh <-chan *rtp.Packet,
	prevAU []*rtp.Packet,
	prevDesc govpx.VP9RTPPayloadDescriptor,
	wantLayers int,
) govpx.VP9RTPPayloadDescriptor {
	t.Helper()
	if len(prevAU) == 0 {
		t.Fatal("spatial-layer wait started without previous RTP access unit")
	}
	const maxAccessUnits = defaultFPS
	for attempt := 0; attempt < maxAccessUnits; attempt++ {
		au := readVP9RTPAccessUnitForTest(t, ctx, rtpCh)
		activeLayers := rtpAccessUnitSpatialLayerCountForTest(t, au)
		first, _, err := govpx.ParseVP9RTPPayloadDescriptor(au[0].Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor spatial-cap AU: %v", err)
		}
		desc := assertWebRTCRTPAccessUnitForTest(t, au, activeLayers,
			first.ScalabilityStructurePresent)
		prevLastSeq := prevAU[0].SequenceNumber + uint16(len(prevAU)-1)
		if got, want := au[0].SequenceNumber, prevLastSeq+1; got != want {
			t.Fatalf("spatial-cap RTP access unit first sequence = %d, want %d",
				got, want)
		}
		assertRTPMediaTimestampAdvancedForTest(t, "spatial-cap RTP access unit",
			prevAU[0].Timestamp, au[0].Timestamp, maxAccessUnits)
		if got, want := desc.PictureID, govpx.NextVP9RTPPictureID(prevDesc.PictureID); got != want {
			t.Fatalf("spatial-cap RTP picture ID = %d, want %d", got, want)
		}
		if desc.ScalabilityStructurePresent &&
			desc.ScalabilityStructure.SpatialLayerCount == wantLayers {
			return desc
		}
		prevAU = au
		prevDesc = desc
	}
	t.Fatalf("spatial-cap control did not produce an active %d-layer key RTP access unit within %d frames",
		wantLayers, maxAccessUnits)
	return govpx.VP9RTPPayloadDescriptor{}
}

func rtpAccessUnitSpatialLayerCountForTest(
	t *testing.T,
	packets []*rtp.Packet,
) int {
	t.Helper()
	maxLayer := -1
	for i, packet := range packets {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(packet.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor layer-count[%d]: %v", i, err)
		}
		if !desc.LayerIndicesPresent {
			t.Fatalf("RTP packet %d missing VP9 layer metadata", i)
		}
		if int(desc.SpatialID) > maxLayer {
			maxLayer = int(desc.SpatialID)
		}
	}
	if maxLayer < 0 {
		t.Fatal("RTP access unit had no spatial layers")
	}
	return maxLayer + 1
}

func assertWebRTCRTPAccessUnitForTest(
	t *testing.T,
	packets []*rtp.Packet,
	spatialLayers int,
	wantSS bool,
) govpx.VP9RTPPayloadDescriptor {
	t.Helper()
	if len(packets) == 0 {
		t.Fatal("empty RTP access unit")
	}
	firstSeq := packets[0].SequenceNumber
	timestamp := packets[0].Timestamp
	var firstDesc govpx.VP9RTPPayloadDescriptor
	var pictureID uint16
	var seenPictureID bool
	var seenLayerStart [govpx.VP9RTPMaxSpatialLayers]bool
	for i, packet := range packets {
		if got, want := packet.SequenceNumber, firstSeq+uint16(i); got != want {
			t.Fatalf("RTP packet %d sequence = %d, want %d", i, got, want)
		}
		if packet.Timestamp != timestamp {
			t.Fatalf("RTP packet %d timestamp = %d, want AU timestamp %d",
				i, packet.Timestamp, timestamp)
		}
		if got, want := packet.Marker, i == len(packets)-1; got != want {
			t.Fatalf("RTP packet %d marker = %t, want %t", i, got, want)
		}
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(packet.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if i == 0 {
			firstDesc = desc
			if !desc.StartOfFrame || desc.SpatialID != 0 {
				t.Fatalf("first RTP packet descriptor = start:%t sid:%d, want base layer start",
					desc.StartOfFrame, desc.SpatialID)
			}
		}
		if !desc.PictureIDPresent || !desc.PictureID15Bit {
			t.Fatalf("RTP packet %d PictureID = present:%t 15bit:%t, want 15-bit",
				i, desc.PictureIDPresent, desc.PictureID15Bit)
		}
		if desc.FlexibleMode {
			t.Fatalf("RTP packet %d used flexible VP9 descriptor", i)
		}
		if !seenPictureID {
			pictureID = desc.PictureID
			seenPictureID = true
		} else if desc.PictureID != pictureID {
			t.Fatalf("RTP packet %d PictureID = %d, want AU PictureID %d",
				i, desc.PictureID, pictureID)
		}
		if !desc.LayerIndicesPresent || int(desc.SpatialID) >= spatialLayers {
			t.Fatalf("RTP packet %d layer metadata = present:%t sid:%d, want sid < %d",
				i, desc.LayerIndicesPresent, desc.SpatialID, spatialLayers)
		}
		if desc.StartOfFrame {
			seenLayerStart[desc.SpatialID] = true
			if desc.SpatialID == 0 && wantSS {
				if !desc.ScalabilityStructurePresent ||
					desc.ScalabilityStructure.SpatialLayerCount != spatialLayers {
					t.Fatalf("base RTP SS = present:%t layers:%d, want %d active layers",
						desc.ScalabilityStructurePresent,
						desc.ScalabilityStructure.SpatialLayerCount,
						spatialLayers)
				}
				if !desc.ScalabilityStructure.PictureGroupPresent {
					t.Fatalf("base RTP non-flexible SS missing GOF")
				}
			} else if desc.ScalabilityStructurePresent {
				t.Fatalf("RTP packet %d layer %d repeated scalability structure",
					i, desc.SpatialID)
			}
			if desc.SpatialID > 0 && !desc.InterLayerDependency {
				t.Fatalf("RTP packet %d enhancement layer %d missing inter-layer dependency",
					i, desc.SpatialID)
			}
		} else if desc.ScalabilityStructurePresent {
			t.Fatalf("RTP packet %d repeated scalability structure on non-start fragment", i)
		}
	}
	for layer := 0; layer < spatialLayers; layer++ {
		if !seenLayerStart[layer] {
			t.Fatalf("RTP access unit missing spatial layer %d start", layer)
		}
	}
	lastDesc, _, err := govpx.ParseVP9RTPPayloadDescriptor(packets[len(packets)-1].Payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor[last]: %v", err)
	}
	if int(lastDesc.SpatialID) != spatialLayers-1 || !lastDesc.EndOfFrame {
		t.Fatalf("last RTP descriptor = sid:%d end:%t, want top layer end",
			lastDesc.SpatialID, lastDesc.EndOfFrame)
	}
	return firstDesc
}

func TestSplitBitrateTreatsBitrateAsTotalBudget(t *testing.T) {
	got := splitBitrate(defaultBitrateKbps, layerSplitPct)
	want := [spatialLayerCount]int{96, 288, 416}
	if got != want {
		t.Fatalf("splitBitrate(%d) = %v, want %v",
			defaultBitrateKbps, got, want)
	}

	total := 0
	for i, kbps := range got {
		if kbps < minLayerBitrateKbps {
			t.Fatalf("layer %d bitrate = %d, want at least %d",
				i, kbps, minLayerBitrateKbps)
		}
		total += kbps
	}
	if total != defaultBitrateKbps {
		t.Fatalf("split total = %d, want %d", total, defaultBitrateKbps)
	}
}

func TestRTCPRequestsKeyFrameOnlyForPLIAndFIR(t *testing.T) {
	rr := marshalRTCPForTest(t, &rtcp.ReceiverReport{SSRC: 1})
	if rtcpRequestsKeyFrame(rr) {
		t.Fatal("receiver report unexpectedly requested keyframe")
	}

	pli := marshalRTCPForTest(t, &rtcp.PictureLossIndication{
		SenderSSRC: 1,
		MediaSSRC:  2,
	})
	if !rtcpRequestsKeyFrame(pli) {
		t.Fatal("PLI did not request keyframe")
	}

	fir := marshalRTCPForTest(t, &rtcp.FullIntraRequest{
		SenderSSRC: 1,
		MediaSSRC:  2,
		FIR: []rtcp.FIREntry{{
			SSRC:           2,
			SequenceNumber: 1,
		}},
	})
	if !rtcpRequestsKeyFrame(fir) {
		t.Fatal("FIR did not request keyframe")
	}

	compound, err := rtcp.Marshal([]rtcp.Packet{
		&rtcp.ReceiverReport{SSRC: 1},
		&rtcp.PictureLossIndication{SenderSSRC: 1, MediaSSRC: 2},
	})
	if err != nil {
		t.Fatalf("marshal compound RTCP: %v", err)
	}
	if !rtcpRequestsKeyFrame(compound) {
		t.Fatal("compound RTCP with PLI did not request keyframe")
	}

	if rtcpRequestsKeyFrame([]byte{0x80}) {
		t.Fatal("malformed RTCP unexpectedly requested keyframe")
	}
}

func TestRTCPParsedPacketsRequestKeyFrame(t *testing.T) {
	if rtcpPacketsRequestKeyFrame([]rtcp.Packet{
		&rtcp.ReceiverReport{SSRC: 1},
	}) {
		t.Fatal("receiver report unexpectedly requested keyframe")
	}
	if rtcpPacketsRequestKeyFrame([]rtcp.Packet{
		&rtcp.TransportLayerNack{
			SenderSSRC: 1,
			MediaSSRC:  2,
			Nacks: []rtcp.NackPair{{
				PacketID:    17,
				LostPackets: 0,
			}},
		},
	}) {
		t.Fatal("transport NACK unexpectedly requested keyframe")
	}

	if !rtcpPacketsRequestKeyFrame([]rtcp.Packet{
		&rtcp.ReceiverReport{SSRC: 1},
		&rtcp.PictureLossIndication{SenderSSRC: 1, MediaSSRC: 2},
	}) {
		t.Fatal("parsed packet list with PLI did not request keyframe")
	}

	if !rtcpPacketsRequestKeyFrame([]rtcp.Packet{
		&rtcp.FullIntraRequest{
			SenderSSRC: 1,
			MediaSSRC:  2,
			FIR: []rtcp.FIREntry{{
				SSRC:           2,
				SequenceNumber: 7,
			}},
		},
	}) {
		t.Fatal("parsed packet list with FIR did not request keyframe")
	}
}

func TestApplyControlResumeRequestsKeyFrame(t *testing.T) {
	ctl := &controlState{}
	ctl.paused.Store(true)

	applyControl(ctl, controlMessage{Type: "pause", Paused: false}, demoConfig{})

	if ctl.paused.Load() {
		t.Fatal("resume control left encoder paused")
	}
	if !ctl.forceKey.Load() {
		t.Fatal("resume control did not request a keyframe")
	}
}

func TestApplyControlSpatialCapUsesWebRTCKeyFramePolicy(t *testing.T) {
	ctl := &controlState{}
	ctl.spatialCap.Store(int32(spatialLayerCount))

	applyControl(ctl, controlMessage{Type: "spatial", Cap: spatialLayerCount},
		demoConfig{})
	if ctl.forceKey.Load() {
		t.Fatal("unchanged spatial cap requested a keyframe")
	}

	applyControl(ctl, controlMessage{Type: "spatial", Cap: 1}, demoConfig{})
	if !ctl.forceKey.Load() {
		t.Fatal("spatial cap decrease did not request a keyframe")
	}

	ctl.forceKey.Store(false)
	applyControl(ctl, controlMessage{Type: "spatial", Cap: 1}, demoConfig{})
	if ctl.forceKey.Load() {
		t.Fatal("repeated spatial cap requested a keyframe")
	}

	applyControl(ctl, controlMessage{Type: "spatial", Cap: spatialLayerCount},
		demoConfig{})
	if !ctl.forceKey.Load() {
		t.Fatal("spatial cap increase did not request a keyframe")
	}
}

func TestApplyControlScreenModeChangeRequestsKeyFrame(t *testing.T) {
	ctl := &controlState{}
	ctl.screenMode.Store(0)

	applyControl(ctl, controlMessage{Type: "screen", Mode: 0}, demoConfig{})
	if ctl.forceKey.Load() {
		t.Fatal("unchanged screen mode requested a keyframe")
	}

	applyControl(ctl, controlMessage{Type: "screen", Mode: 1}, demoConfig{})
	if !ctl.forceKey.Load() {
		t.Fatal("screen mode change did not request a keyframe")
	}

	ctl.forceKey.Store(false)
	applyControl(ctl, controlMessage{Type: "screen", Mode: 99}, demoConfig{})
	if got := ctl.screenMode.Load(); got != 1 {
		t.Fatalf("invalid screen mode changed setting to %d", got)
	}
	if ctl.forceKey.Load() {
		t.Fatal("invalid screen mode requested a keyframe")
	}
}

func TestApplyControlLocalWithholdQueuesAccessUnits(t *testing.T) {
	ctl := &controlState{}

	applyControl(ctl, controlMessage{Type: "withhold"}, demoConfig{})
	if got := ctl.withholdAUs.Load(); got != 1 {
		t.Fatalf("default withhold count = %d, want 1", got)
	}
	if ctl.forceKey.Load() {
		t.Fatal("local withhold control pre-forced a keyframe")
	}

	applyControl(ctl, controlMessage{Type: "withhold", Count: 2}, demoConfig{})
	if got := ctl.withholdAUs.Load(); got != 3 {
		t.Fatalf("queued withhold count = %d, want 3", got)
	}

	applyControl(ctl, controlMessage{Type: "withhold", Count: 99}, demoConfig{})
	if got := ctl.withholdAUs.Load(); got != 6 {
		t.Fatalf("clamped withhold count = %d, want 6", got)
	}
}

func TestApplyControlLocalPartialWriteQueuesAccessUnits(t *testing.T) {
	ctl := &controlState{}

	applyControl(ctl, controlMessage{Type: "partial-write"}, demoConfig{})
	if got := ctl.partialWriteAUs.Load(); got != 1 {
		t.Fatalf("default partial-write count = %d, want 1", got)
	}
	if ctl.forceKey.Load() {
		t.Fatal("local partial-write control pre-forced a keyframe")
	}

	applyControl(ctl, controlMessage{Type: "partial-write", Count: 2},
		demoConfig{})
	if got := ctl.partialWriteAUs.Load(); got != 3 {
		t.Fatalf("queued partial-write count = %d, want 3", got)
	}

	applyControl(ctl, controlMessage{Type: "partial-write", Count: 99},
		demoConfig{})
	if got := ctl.partialWriteAUs.Load(); got != 6 {
		t.Fatalf("clamped partial-write count = %d, want 6", got)
	}
}

func TestConsumeLocalWithholdAccessUnit(t *testing.T) {
	ctl := &controlState{}
	ctl.withholdAUs.Store(2)

	if !consumeLocalWithholdAccessUnit(ctl) ||
		!consumeLocalWithholdAccessUnit(ctl) {
		t.Fatal("queued local withhold was not consumed")
	}
	if consumeLocalWithholdAccessUnit(ctl) {
		t.Fatal("empty local withhold queue consumed an access unit")
	}
	if got := ctl.withholdAUs.Load(); got != 0 {
		t.Fatalf("withhold queue after consume = %d, want 0", got)
	}
}

func TestConsumeLocalPartialWriteAccessUnit(t *testing.T) {
	ctl := &controlState{}
	ctl.partialWriteAUs.Store(2)

	if !consumeLocalPartialWriteAccessUnit(ctl) ||
		!consumeLocalPartialWriteAccessUnit(ctl) {
		t.Fatal("queued local partial write was not consumed")
	}
	if consumeLocalPartialWriteAccessUnit(ctl) {
		t.Fatal("empty local partial write queue consumed an access unit")
	}
	if got := ctl.partialWriteAUs.Load(); got != 0 {
		t.Fatalf("partial-write queue after consume = %d, want 0", got)
	}
}

func TestApplyControlPauseDoesNotClearPendingKeyFrame(t *testing.T) {
	ctl := &controlState{}
	ctl.forceKey.Store(true)

	applyControl(ctl, controlMessage{Type: "pause", Paused: true}, demoConfig{})

	if !ctl.paused.Load() {
		t.Fatal("pause control did not pause encoder")
	}
	if !ctl.forceKey.Load() {
		t.Fatal("pause control cleared pending keyframe request")
	}
}

func TestConsumeForceKeyForActiveAccessUnitPreservesPausedRequest(t *testing.T) {
	ctl := &controlState{}
	ctl.paused.Store(true)
	ctl.forceKey.Store(true)

	active, forceKey := consumeForceKeyForActiveAccessUnit(ctl)
	if active || forceKey {
		t.Fatalf("paused access unit = active:%t forceKey:%t, want false/false",
			active, forceKey)
	}
	if !ctl.forceKey.Load() {
		t.Fatal("paused access unit consumed pending keyframe request")
	}
}

func TestConsumeForceKeyForActiveAccessUnitConsumesActiveRequest(t *testing.T) {
	ctl := &controlState{}
	ctl.forceKey.Store(true)

	active, forceKey := consumeForceKeyForActiveAccessUnit(ctl)
	if !active || !forceKey {
		t.Fatalf("active access unit = active:%t forceKey:%t, want true/true",
			active, forceKey)
	}
	if ctl.forceKey.Load() {
		t.Fatal("active access unit left keyframe request pending")
	}
}

func TestConsumeForceKeyForWebRTCAccessUnitHonorsPacketizerRecovery(t *testing.T) {
	ctl := &controlState{}
	packetizer := govpx.NewVP9WebRTCPacketizer(17)
	packetizer.MarkAccessUnitUnsent()

	active, forceKey := consumeForceKeyForWebRTCAccessUnit(ctl, &packetizer)
	if !active || !forceKey {
		t.Fatalf("WebRTC access unit = active:%t forceKey:%t, want true/true",
			active, forceKey)
	}
	if ctl.forceKey.Load() {
		t.Fatal("packetizer recovery request left a duplicate control key pending")
	}
	if !packetizer.NeedsKeyFrame() {
		t.Fatal("packetizer recovery request was cleared before recovery key packetized")
	}
}

func TestConsumeForceKeyForWebRTCAccessUnitPreservesPausedRecovery(t *testing.T) {
	ctl := &controlState{}
	ctl.paused.Store(true)
	ctl.forceKey.Store(true)
	packetizer := govpx.NewVP9WebRTCPacketizer(17)
	packetizer.MarkAccessUnitUnsent()

	active, forceKey := consumeForceKeyForWebRTCAccessUnit(ctl, &packetizer)
	if active || forceKey {
		t.Fatalf("paused WebRTC access unit = active:%t forceKey:%t, want false/false",
			active, forceKey)
	}
	if !ctl.forceKey.Load() {
		t.Fatal("paused WebRTC access unit consumed pending control key")
	}
	if !packetizer.NeedsKeyFrame() {
		t.Fatal("paused WebRTC access unit cleared packetizer recovery request")
	}
}

func TestRequestKeyFrameAfterFailedAccessUnitRequeuesKey(t *testing.T) {
	ctl := &controlState{}

	requestKeyFrameAfterFailedAccessUnit(ctl)

	if !ctl.forceKey.Load() {
		t.Fatal("failed access unit did not queue keyframe request")
	}
}

func TestRequestKeyFrameAfterFailedAccessUnitPreservesPendingKey(t *testing.T) {
	ctl := &controlState{}
	ctl.forceKey.Store(true)

	requestKeyFrameAfterFailedAccessUnit(ctl)

	if !ctl.forceKey.Load() {
		t.Fatal("failed access unit cleared pending keyframe request")
	}
}

func TestRequestKeyFrameAfterFailedEncodedAccessUnitConsumesPictureID(t *testing.T) {
	ctl := &controlState{}
	packetizer := govpx.NewVP9WebRTCPacketizer(
		govpx.VP9RTPPictureID15BitMask)

	requestKeyFrameAfterFailedEncodedAccessUnit(ctl, &packetizer)

	if !ctl.forceKey.Load() {
		t.Fatal("failed encoded access unit did not queue keyframe request")
	}
	if !packetizer.NeedsKeyFrame() {
		t.Fatal("failed encoded access unit did not require packetizer recovery")
	}
	if got := packetizer.PictureID(); got != 0 {
		t.Fatalf("failed encoded access unit PictureID = %d, want wrap to 0",
			got)
	}
}

func TestRequestKeyFrameAfterFailedEncodedAccessUnitAllowsNilPacketizer(t *testing.T) {
	ctl := &controlState{}

	requestKeyFrameAfterFailedEncodedAccessUnit(ctl, nil)

	if !ctl.forceKey.Load() {
		t.Fatal("failed encoded access unit with nil packetizer did not queue keyframe request")
	}
}

func TestRequestKeyFrameAfterUnsentAccessUnitKeepsPictureID(t *testing.T) {
	ctl := &controlState{}
	packetizer := govpx.NewVP9WebRTCPacketizer(0x230)
	pictureID := packetizer.PictureID()

	requestKeyFrameAfterUnsentAccessUnit(ctl, &packetizer)

	if !ctl.forceKey.Load() {
		t.Fatal("unsent access unit did not queue keyframe request")
	}
	if !packetizer.NeedsKeyFrame() {
		t.Fatal("unsent access unit did not require packetizer recovery")
	}
	if got := packetizer.PictureID(); got != pictureID {
		t.Fatalf("unsent access unit PictureID = %d, want unchanged %d",
			got, pictureID)
	}
}

func TestWriteWebRTCRTPAccessUnitAssignsSequenceTimestampAndMarker(t *testing.T) {
	writer := &recordingRTPWriterForTest{failAt: -1}
	sequence := uint16(0xfffe)
	fragments := []govpx.RTPPayloadFragment{
		{Payload: []byte{0x81, 0x01}},
		{Payload: []byte{0x82, 0x02}, Marker: true},
	}

	written, err := writeWebRTCRTPAccessUnit(writer, fragments, 0x12345678,
		&sequence)
	if err != nil {
		t.Fatalf("writeWebRTCRTPAccessUnit: %v", err)
	}
	if written != len(fragments) {
		t.Fatalf("written packets = %d, want %d", written, len(fragments))
	}
	if sequence != 0 {
		t.Fatalf("next RTP sequence = %d, want wrap to 0", sequence)
	}
	if len(writer.packets) != len(fragments) {
		t.Fatalf("captured RTP packets = %d, want %d",
			len(writer.packets), len(fragments))
	}
	for i, packet := range writer.packets {
		if got, want := packet.SequenceNumber,
			uint16(0xfffe+i); got != want {
			t.Fatalf("packet %d sequence = %d, want %d", i, got, want)
		}
		if packet.Timestamp != 0x12345678 {
			t.Fatalf("packet %d timestamp = %d, want %d", i,
				packet.Timestamp, uint32(0x12345678))
		}
		if packet.Marker != fragments[i].Marker {
			t.Fatalf("packet %d marker = %t, want %t",
				i, packet.Marker, fragments[i].Marker)
		}
		if !bytes.Equal(packet.Payload, fragments[i].Payload) {
			t.Fatalf("packet %d payload = %x, want %x",
				i, packet.Payload, fragments[i].Payload)
		}
	}
}

func TestWriteWebRTCRTPAccessUnitFailureLeavesUnsentRecovery(t *testing.T) {
	writer := &recordingRTPWriterForTest{
		failAt: 1,
		err:    io.ErrUnexpectedEOF,
	}
	sequence := uint16(41)
	fragments := []govpx.RTPPayloadFragment{
		{Payload: []byte{0x81, 0x01}},
		{Payload: []byte{0x82, 0x02}, Marker: true},
	}

	written, err := writeWebRTCRTPAccessUnit(writer, fragments, 0x12345678,
		&sequence)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("writeWebRTCRTPAccessUnit err = %v, want ErrUnexpectedEOF",
			err)
	}
	if written != 1 {
		t.Fatalf("written packets before failure = %d, want 1", written)
	}
	if sequence != 42 {
		t.Fatalf("next RTP sequence after failure = %d, want 42", sequence)
	}

	ctl := &controlState{}
	packetizer := govpx.NewVP9WebRTCPacketizer(0x230)
	pictureID := packetizer.PictureID()
	requestKeyFrameAfterUnsentAccessUnit(ctl, &packetizer)
	if !ctl.forceKey.Load() || !packetizer.NeedsKeyFrame() {
		t.Fatalf("unsent RTP write recovery = force:%t packetizer:%t, want true/true",
			ctl.forceKey.Load(), packetizer.NeedsKeyFrame())
	}
	if got := packetizer.PictureID(); got != pictureID {
		t.Fatalf("unsent RTP write PictureID = %d, want unchanged %d",
			got, pictureID)
	}
}

func TestPartialWriteRTPWriterFailsAfterPrefix(t *testing.T) {
	inner := &recordingRTPWriterForTest{failAt: -1}
	writer := &partialWriteRTPWriter{
		inner:     inner,
		failAfter: 1,
		err:       io.ErrUnexpectedEOF,
	}
	sequence := uint16(9)
	fragments := []govpx.RTPPayloadFragment{
		{Payload: []byte{0x81, 0x01}},
		{Payload: []byte{0x82, 0x02}, Marker: true},
	}

	written, err := writeWebRTCRTPAccessUnit(writer, fragments, 0x12345678,
		&sequence)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("partial write err = %v, want ErrUnexpectedEOF", err)
	}
	if written != 1 || len(inner.packets) != 1 {
		t.Fatalf("partial write packets = written:%d captured:%d, want 1/1",
			written, len(inner.packets))
	}
	if inner.packets[0].Marker {
		t.Fatal("partial write emitted RTP marker on prefix packet")
	}
	if sequence != 10 {
		t.Fatalf("partial write next sequence = %d, want 10", sequence)
	}
}

func TestSpatialCapForAccessUnitDefersPendingCapUntilKeyFrame(t *testing.T) {
	ctl := &controlState{}
	ctl.spatialCap.Store(1)

	if got := spatialCapForAccessUnit(ctl, spatialLayerCount, false); got != spatialLayerCount {
		t.Fatalf("ordinary access unit cap = %d, want current cap %d",
			got, spatialLayerCount)
	}
	if got := spatialCapForAccessUnit(ctl, spatialLayerCount, true); got != 1 {
		t.Fatalf("forced access unit cap = %d, want pending cap 1", got)
	}
}

func TestSpatialCapChangeAfterForceKeyConsumedIsAppliedNextKeyFrame(t *testing.T) {
	ctl := &controlState{}
	ctl.spatialCap.Store(int32(spatialLayerCount))

	currentCap := spatialCapForAccessUnit(ctl, spatialLayerCount, false)
	applyControl(ctl, controlMessage{Type: "spatial", Cap: 1}, demoConfig{})

	if currentCap != spatialLayerCount {
		t.Fatalf("current access unit cap = %d, want old cap %d",
			currentCap, spatialLayerCount)
	}
	active, forceKey := consumeForceKeyForActiveAccessUnit(ctl)
	if !active || !forceKey {
		t.Fatalf("next access unit = active:%t forceKey:%t, want true/true",
			active, forceKey)
	}
	if nextCap := spatialCapForAccessUnit(ctl, currentCap, forceKey); nextCap != 1 {
		t.Fatalf("next forced access unit cap = %d, want 1", nextCap)
	}
}

func TestSpatialCapBackoffDownshiftsAfterRepeatedOverruns(t *testing.T) {
	backoff := newSpatialCapBackoff(spatialLayerCount)
	interval := time.Second / time.Duration(defaultFPS)
	overrun := interval + interval*2/3

	for i := 0; i < spatialCapBackoffOverruns-1; i++ {
		if backoff.observe(spatialLayerCount, spatialLayerCount, overrun, interval) {
			t.Fatalf("overrun %d requested cap change too early", i)
		}
	}
	if !backoff.observe(spatialLayerCount, spatialLayerCount, overrun, interval) {
		t.Fatal("repeated overruns did not request a cap change")
	}
	if backoff.maxCap != spatialLayerCount-1 {
		t.Fatalf("backoff max cap = %d, want %d",
			backoff.maxCap, spatialLayerCount-1)
	}
	if got := backoff.effectiveCap(spatialLayerCount); got != spatialLayerCount-1 {
		t.Fatalf("effective cap after backoff = %d, want %d",
			got, spatialLayerCount-1)
	}
}

func TestSpatialCapBackoffAllowsNearBudgetJitter(t *testing.T) {
	backoff := newSpatialCapBackoff(spatialLayerCount)
	interval := time.Second / time.Duration(defaultFPS)
	nearBudget := interval + interval/3

	for i := 0; i < spatialCapBackoffOverruns+1; i++ {
		if backoff.observe(spatialLayerCount, spatialLayerCount, nearBudget, interval) {
			t.Fatalf("near-budget frame %d requested cap change", i)
		}
	}
	if backoff.maxCap != spatialLayerCount || backoff.overrunStreak != 0 {
		t.Fatalf("near-budget jitter changed backoff = %+v, want cap %d streak 0",
			backoff, spatialLayerCount)
	}
}

func TestSpatialCapBackoffDownshiftsBeforeEncodeOnRepeatedLateStarts(t *testing.T) {
	backoff := newSpatialCapBackoff(spatialLayerCount)
	interval := time.Second / time.Duration(defaultFPS)
	lateStart := interval + interval*2/3

	if changed, counted := backoff.observeLateStart(
		spatialLayerCount, spatialLayerCount, interval/2, interval); changed || counted {
		t.Fatalf("stable start = changed:%t counted:%t, want false/false",
			changed, counted)
	}
	for i := 0; i < spatialCapBackoffOverruns-1; i++ {
		changed, counted := backoff.observeLateStart(
			spatialLayerCount, spatialLayerCount, lateStart, interval)
		if !counted || changed {
			t.Fatalf("late start %d = changed:%t counted:%t, want false/true",
				i, changed, counted)
		}
	}
	changed, counted := backoff.observeLateStart(
		spatialLayerCount, spatialLayerCount, lateStart, interval)
	if !changed || !counted {
		t.Fatalf("repeated late starts = changed:%t counted:%t, want true/true",
			changed, counted)
	}
	if got := backoff.effectiveCap(spatialLayerCount); got != spatialLayerCount-1 {
		t.Fatalf("effective cap after late starts = %d, want %d",
			got, spatialLayerCount-1)
	}
}

func TestSpatialCapBackoffCountsOneStrikePerLateAccessUnit(t *testing.T) {
	backoff := newSpatialCapBackoff(spatialLayerCount)
	interval := time.Second / time.Duration(defaultFPS)
	overrun := interval + interval*2/3

	changed, counted := backoff.observeLateStart(
		spatialLayerCount, spatialLayerCount, overrun, interval)
	if changed || !counted {
		t.Fatalf("first late start = changed:%t counted:%t, want false/true",
			changed, counted)
	}
	if !counted && backoff.observe(spatialLayerCount, spatialLayerCount, overrun, interval) {
		t.Fatal("post-encode observe unexpectedly changed cap")
	}
	if backoff.overrunStreak != 1 {
		t.Fatalf("overrun streak after one late access unit = %d, want 1",
			backoff.overrunStreak)
	}
}

func TestSpatialCapBackoffDoesNotCompoundModerateLagAndEncode(t *testing.T) {
	backoff := newSpatialCapBackoff(spatialLayerCount)
	interval := time.Second / time.Duration(defaultFPS)
	moderateLateStart := interval + interval/10
	nearBudgetEncode := interval + interval/5

	for i := 0; i < spatialCapBackoffOverruns+1; i++ {
		changed, counted := backoff.observeLateStartForAccessUnit(
			spatialLayerCount, spatialLayerCount, moderateLateStart, interval, false)
		if changed || counted {
			t.Fatalf("moderate late start %d = changed:%t counted:%t, want false/false",
				i, changed, counted)
		}
		if backoff.observeCompletedAccessUnit(spatialLayerCount, spatialLayerCount,
			nearBudgetEncode, interval, false, counted) {
			t.Fatalf("moderate late+encode frame %d requested cap change", i)
		}
	}
	if backoff.maxCap != spatialLayerCount || backoff.overrunStreak != 0 {
		t.Fatalf("moderate late+encode changed backoff = %+v, want cap %d streak 0",
			backoff, spatialLayerCount)
	}
}

func TestSpatialCapBackoffIgnoresForcedKeyAccessUnits(t *testing.T) {
	backoff := newSpatialCapBackoff(spatialLayerCount)
	interval := time.Second / time.Duration(defaultFPS)
	overrun := interval + interval*2/3

	for i := 0; i < spatialCapBackoffOverruns+1; i++ {
		changed, counted := backoff.observeLateStartForAccessUnit(
			spatialLayerCount, spatialLayerCount, overrun, interval, true)
		if changed || counted {
			t.Fatalf("forced-key late start %d = changed:%t counted:%t, want false/false",
				i, changed, counted)
		}
		if backoff.observeCompletedAccessUnit(spatialLayerCount, spatialLayerCount,
			overrun, interval, true, false) {
			t.Fatalf("forced-key completion %d requested cap change", i)
		}
	}
	if backoff.maxCap != spatialLayerCount || backoff.overrunStreak != 0 {
		t.Fatalf("forced-key overruns changed backoff = %+v, want cap %d streak 0",
			backoff, spatialLayerCount)
	}
	for i := 0; i < spatialCapBackoffOverruns-1; i++ {
		if backoff.observeCompletedAccessUnit(spatialLayerCount, spatialLayerCount,
			overrun, interval, false, false) {
			t.Fatalf("non-key overrun %d requested cap change too early", i)
		}
	}
	if !backoff.observeCompletedAccessUnit(spatialLayerCount, spatialLayerCount,
		overrun, interval, false, false) {
		t.Fatal("non-key overruns did not request cap change")
	}
	if backoff.maxCap != spatialLayerCount-1 {
		t.Fatalf("non-key overruns after forced keys left cap = %d, want %d",
			backoff.maxCap, spatialLayerCount-1)
	}
}

func TestSpatialCapBackoffRecoversTowardRequestedCapAfterStableFrames(t *testing.T) {
	backoff := newSpatialCapBackoff(spatialLayerCount)
	interval := time.Second / time.Duration(defaultFPS)
	overrun := interval + interval*2/3
	for i := 0; i < spatialCapBackoffOverruns; i++ {
		_ = backoff.observe(spatialLayerCount, spatialLayerCount, overrun, interval)
	}
	if backoff.maxCap != spatialLayerCount-1 {
		t.Fatalf("test setup max cap = %d, want %d",
			backoff.maxCap, spatialLayerCount-1)
	}

	stable := interval / 2
	for i := 0; i < spatialCapBackoffRecoveryFrames-1; i++ {
		if backoff.observe(spatialLayerCount-1, spatialLayerCount, stable, interval) {
			t.Fatalf("stable frame %d recovered too early", i)
		}
	}
	if !backoff.observe(spatialLayerCount-1, spatialLayerCount, stable, interval) {
		t.Fatal("stable frames did not request recovery")
	}
	if backoff.maxCap != spatialLayerCount {
		t.Fatalf("recovered max cap = %d, want %d",
			backoff.maxCap, spatialLayerCount)
	}
	if got := backoff.effectiveCap(spatialLayerCount); got != spatialLayerCount {
		t.Fatalf("effective cap after recovery = %d, want %d",
			got, spatialLayerCount)
	}
}

func TestSpatialCapBackoffManualCapChangeAppliesOnForcedKey(t *testing.T) {
	backoff := newSpatialCapBackoff(spatialLayerCount)
	interval := time.Second / time.Duration(defaultFPS)
	overrun := interval + interval*2/3
	for i := 0; i < spatialCapBackoffOverruns; i++ {
		_ = backoff.observe(spatialLayerCount, spatialLayerCount, overrun, interval)
	}
	if got := backoff.effectiveCap(1); got != 1 {
		t.Fatalf("manual cap down effective cap = %d, want 1", got)
	}
	if backoff.maxCap != 1 {
		t.Fatalf("manual cap down max cap = %d, want 1", backoff.maxCap)
	}
	if got := backoff.effectiveCap(spatialLayerCount); got != spatialLayerCount {
		t.Fatalf("manual cap up effective cap = %d, want %d",
			got, spatialLayerCount)
	}
	if backoff.maxCap != spatialLayerCount {
		t.Fatalf("manual cap up max cap = %d, want %d",
			backoff.maxCap, spatialLayerCount)
	}
}

func TestRTPClockOffsetAvoidsNonDivisorFPSDrift(t *testing.T) {
	const fps = 29
	naiveAfterOneSecond := uint64(fps) * uint64(rtpClockHz/fps)
	if naiveAfterOneSecond == rtpClockHz {
		t.Fatal("test setup expected integer-division RTP clock drift")
	}
	if got := rtpClockOffset(uint64(fps), fps); got != rtpClockHz {
		t.Fatalf("rtpClockOffset(%d frames @ %dfps) = %d, want %d",
			fps, fps, got, rtpClockHz)
	}

	var sawLongStep bool
	for frame := uint64(1); frame <= fps; frame++ {
		prev := rtpClockOffset(frame-1, fps)
		next := rtpClockOffset(frame, fps)
		step := next - prev
		if step != uint64(rtpClockHz/fps) && step != uint64(rtpClockHz/fps+1) {
			t.Fatalf("rtp clock step %d = %d, want %d or %d",
				frame, step, rtpClockHz/fps, rtpClockHz/fps+1)
		}
		if step == uint64(rtpClockHz/fps+1) {
			sawLongStep = true
		}
	}
	if !sawLongStep {
		t.Fatal("rtp clock never compensated for fractional frame duration")
	}
}

func TestRTPMediaFrameForTickSkipsMissedIntervals(t *testing.T) {
	startedAt := time.Unix(100, 0)
	interval := time.Second / time.Duration(defaultFPS)

	first := rtpMediaFrameForTick(startedAt, startedAt.Add(interval),
		defaultFPS, 0, false)
	if first != 0 {
		t.Fatalf("first media frame = %d, want 0", first)
	}
	afterStall := rtpMediaFrameForTick(startedAt, startedAt.Add(6*interval),
		defaultFPS, first, true)
	if afterStall != 5 {
		t.Fatalf("media frame after skipped ticks = %d, want 5",
			afterStall)
	}
	if got, want := rtpClockOffset(afterStall, defaultFPS)-
		rtpClockOffset(first, defaultFPS),
		uint64(5*rtpClockHz/defaultFPS); got != want {
		t.Fatalf("RTP timestamp gap after stall = %d, want %d", got, want)
	}

	duplicateTick := rtpMediaFrameForTick(startedAt,
		startedAt.Add(6*interval), defaultFPS, afterStall, true)
	if duplicateTick != afterStall+1 {
		t.Fatalf("duplicate tick media frame = %d, want monotonic %d",
			duplicateTick, afterStall+1)
	}
}

func TestRTPMediaFrameForAccessUnitUsesWakeTimeWhenTickerIsLate(t *testing.T) {
	startedAt := time.Unix(100, 0)
	interval := time.Second / time.Duration(defaultFPS)
	staleTick := startedAt.Add(2 * interval)
	wakeTime := startedAt.Add(7 * interval)

	staleFrame := rtpMediaFrameForTick(startedAt, staleTick,
		defaultFPS, 0, false)
	if staleFrame != 1 {
		t.Fatalf("test setup stale frame = %d, want 1", staleFrame)
	}
	got := rtpMediaFrameForAccessUnit(startedAt, staleTick, wakeTime,
		defaultFPS, 0, false)
	if got != 6 {
		t.Fatalf("late access-unit media frame = %d, want wall-clock frame 6",
			got)
	}
	if got <= staleFrame {
		t.Fatalf("late access-unit frame = %d did not advance past stale ticker frame %d",
			got, staleFrame)
	}
}

func TestAccessUnitTimingHelpersClampNegativeLag(t *testing.T) {
	tick := time.Unix(200, 0)
	started := tick.Add(5 * time.Millisecond)
	if got := accessUnitScheduleLag(tick, started); got != 5*time.Millisecond {
		t.Fatalf("schedule lag = %s, want 5ms", got)
	}
	if got := accessUnitWallElapsed(tick, started.Add(7*time.Millisecond)); got != 12*time.Millisecond {
		t.Fatalf("wall elapsed = %s, want 12ms", got)
	}
	if got := accessUnitScheduleLag(tick, tick.Add(-time.Millisecond)); got != 0 {
		t.Fatalf("negative schedule lag = %s, want 0", got)
	}
	if got := accessUnitWallElapsed(tick, tick.Add(-time.Millisecond)); got != 0 {
		t.Fatalf("negative wall elapsed = %s, want 0", got)
	}
}

func assertRTPMediaTimestampAdvancedForTest(t *testing.T, label string,
	prev, next uint32, maxFrames int,
) {
	t.Helper()
	if maxFrames <= 0 {
		t.Fatalf("%s maxFrames = %d, want positive", label, maxFrames)
	}
	minStep := uint32(rtpClockHz / defaultFPS)
	maxStep := uint32(maxFrames) * minStep
	if got := next - prev; got < minStep || got > maxStep {
		t.Fatalf("%s timestamp step = %d, want [%d,%d]",
			label, got, minStep, maxStep)
	}
}

func TestWaitICEGatheringComplete(t *testing.T) {
	done := make(chan struct{})
	close(done)
	if !waitICEGatheringComplete(done, time.Second) {
		t.Fatal("closed gathering channel reported timeout")
	}
	if !waitICEGatheringComplete(done, 0) {
		t.Fatal("closed gathering channel with no timeout reported timeout")
	}
	if waitICEGatheringComplete(nil, time.Second) {
		t.Fatal("nil gathering channel reported complete")
	}

	open := make(chan struct{})
	start := time.Now()
	if waitICEGatheringComplete(open, time.Millisecond) {
		t.Fatal("open gathering channel reported complete")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout helper took %s, want bounded wait", elapsed)
	}
}

func TestWaitForPeerConnected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connected := make(chan struct{})
	close(connected)
	if !waitForPeerConnected(ctx, connected) {
		t.Fatal("closed connected channel did not open encoder gate")
	}
}

func TestWaitForPeerConnectedReturnsFalseOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	connected := make(chan struct{})
	cancel()
	if waitForPeerConnected(ctx, connected) {
		t.Fatal("canceled context opened encoder gate")
	}
}

func TestRunEncoderAfterConnectedClosesTelemetryBeforeConnected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	connected := make(chan struct{})
	telemetry := make(chan []byte)
	cancel()

	go runEncoderAfterConnected(ctx, connected, nil, telemetry,
		&controlState{}, demoConfig{})

	select {
	case _, ok := <-telemetry:
		if ok {
			t.Fatal("telemetry channel received data before connection")
		}
	case <-time.After(time.Second):
		t.Fatal("telemetry channel was not closed after canceled connection")
	}
}

func TestPeerConnectionDisconnectedDoesNotStopEncoder(t *testing.T) {
	nonTerminal := []webrtc.PeerConnectionState{
		webrtc.PeerConnectionStateNew,
		webrtc.PeerConnectionStateConnecting,
		webrtc.PeerConnectionStateConnected,
		webrtc.PeerConnectionStateDisconnected,
	}
	for _, state := range nonTerminal {
		if peerConnectionStateIsTerminal(state) {
			t.Fatalf("%s unexpectedly treated as terminal", state)
		}
	}
	for _, state := range []webrtc.PeerConnectionState{
		webrtc.PeerConnectionStateFailed,
		webrtc.PeerConnectionStateClosed,
	} {
		if !peerConnectionStateIsTerminal(state) {
			t.Fatalf("%s was not treated as terminal", state)
		}
	}
}

func TestVP9WebRTCCodecCapabilityPinsProfile0AndFeedback(t *testing.T) {
	codec := vp9WebRTCCodecCapability()
	if codec.MimeType != webrtc.MimeTypeVP9 ||
		codec.ClockRate != rtpClockHz ||
		codec.SDPFmtpLine != vp9Profile0Fmtp {
		t.Fatalf("codec capability = %+v, want VP9/%d/%q",
			codec, rtpClockHz, vp9Profile0Fmtp)
	}
	wantFeedback := map[webrtc.RTCPFeedback]bool{
		{Type: webrtc.TypeRTCPFBGoogREMB}:               true,
		{Type: webrtc.TypeRTCPFBCCM, Parameter: "fir"}:  true,
		{Type: webrtc.TypeRTCPFBNACK}:                   true,
		{Type: webrtc.TypeRTCPFBNACK, Parameter: "pli"}: true,
		{Type: webrtc.TypeRTCPFBTransportCC}:            true,
	}
	for _, feedback := range codec.RTCPFeedback {
		delete(wantFeedback, feedback)
	}
	if len(wantFeedback) != 0 {
		t.Fatalf("codec feedback = %+v, missing %+v",
			codec.RTCPFeedback, wantFeedback)
	}
}

func TestPlainVP9WebRTCKeyframeIntervalAvoidsShortPeriodicKeys(t *testing.T) {
	got := plainVP9WebRTCKeyframeInterval(defaultFPS)
	want := plainVP9WebRTCKeyframeIntervalSeconds * defaultFPS
	if got != want {
		t.Fatalf("plain VP9 keyframe interval = %d, want %d", got, want)
	}
	if got <= 128 {
		t.Fatalf("plain VP9 keyframe interval = %d, want beyond libvpx default cadence", got)
	}
}

func TestPlainVP9FlexiblePacketizerHandlesForcedKeyChurn(t *testing.T) {
	enc, err := newPlainVP9Encoder(demoConfig{
		FPS:          25,
		BitrateKbps:  800,
		PlainVP9Mode: true,
	})
	if err != nil {
		t.Fatalf("newPlainVP9Encoder: %v", err)
	}
	defer enc.Close()

	img := image.NewYCbCr(image.Rect(0, 0, plainVP9Width, plainVP9Height),
		image.YCbCrSubsampleRatio420)
	dst := make([]byte, plainVP9FrameBudget())
	packetizer := govpx.NewVP9WebRTCPacketizer(0x120)
	var fragments []govpx.RTPPayloadFragment
	var payloadBuf []byte
	for frame := 0; frame < 180; frame++ {
		if frame == 1 || frame == 2 || (frame != 0 && frame%30 == 0) ||
			frame == 31 {
			enc.ForceKeyFrame()
		}
		drawFrameYCbCr(img, frame+1, 1)
		result, err := enc.EncodeIntoWithResult(img, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", frame, err)
		}
		fragmentCount, payloadBytes, sent, err := plainVP9WebRTCPacketizationSize(
			&packetizer, result, rtpPayloadMTU, true)
		if err != nil {
			t.Fatalf("PacketizationSize[%d]: %v", frame, err)
		}
		if !sent {
			t.Fatalf("PacketizationSize[%d] reported unsent frame", frame)
		}
		if cap(fragments) < fragmentCount {
			fragments = make([]govpx.RTPPayloadFragment, fragmentCount)
		}
		fragments = fragments[:fragmentCount]
		if cap(payloadBuf) < payloadBytes {
			payloadBuf = make([]byte, payloadBytes)
		}
		payloadBuf = payloadBuf[:payloadBytes]
		n, used, sent, err := packetizePlainVP9WebRTCInto(&packetizer,
			result, fragments, payloadBuf, rtpPayloadMTU, true)
		if err != nil || !sent {
			t.Fatalf("PacketizeInto[%d] = packets:%d bytes:%d sent:%t err:%v",
				frame, n, used, sent, err)
		}
		if n != fragmentCount || used != payloadBytes {
			t.Fatalf("PacketizeInto[%d] returned %d/%d, want %d/%d",
				frame, n, used, fragmentCount, payloadBytes)
		}
	}
}

func TestPlainVP9TemporalModeUsesThreeTemporalLayers(t *testing.T) {
	enc, err := newPlainVP9Encoder(demoConfig{
		FPS:                  defaultFPS,
		BitrateKbps:          defaultBitrateKbps,
		PlainVP9Mode:         true,
		PlainVP9TemporalMode: true,
	})
	if err != nil {
		t.Fatalf("newPlainVP9Encoder: %v", err)
	}
	defer enc.Close()

	img := image.NewYCbCr(image.Rect(0, 0, plainVP9Width, plainVP9Height),
		image.YCbCrSubsampleRatio420)
	dst := make([]byte, plainVP9FrameBudget())
	wantTemporalID := []int{0, 2, 1, 2, 0}
	wantTL0 := []uint8{0, 0, 0, 0, 1}
	for frame := range wantTemporalID {
		drawFrameYCbCr(img, frame+1, 1)
		result, err := enc.EncodeIntoWithResult(img, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", frame, err)
		}
		if result.TemporalLayerCount != 3 ||
			result.TemporalLayerID != wantTemporalID[frame] ||
			result.TL0PICIDX != wantTL0[frame] {
			t.Fatalf("frame %d temporal = id:%d count:%d tl0:%d, want id:%d count:3 tl0:%d",
				frame, result.TemporalLayerID, result.TemporalLayerCount,
				result.TL0PICIDX, wantTemporalID[frame], wantTL0[frame])
		}
	}
}

func TestParsePlainVP9TemporalLayeringMode(t *testing.T) {
	cases := []struct {
		name string
		want govpx.TemporalLayeringMode
	}{
		{name: "default", want: plainVP9TemporalMode},
		{name: "six-frame", want: govpx.TemporalLayeringThreeLayersSixFrame},
		{name: "no-inter-layer-prediction", want: govpx.TemporalLayeringThreeLayersNoInterLayerPrediction},
		{name: "layer-one-prediction", want: govpx.TemporalLayeringThreeLayersLayerOnePrediction},
		{name: "with-sync", want: govpx.TemporalLayeringThreeLayersWithSync},
		{name: "altref-with-sync", want: govpx.TemporalLayeringThreeLayersAltRefWithSync},
		{name: "one-reference", want: govpx.TemporalLayeringThreeLayersOneReference},
		{name: "no-sync", want: govpx.TemporalLayeringThreeLayersNoSync},
	}
	for _, tc := range cases {
		got, err := parsePlainVP9TemporalLayeringMode(tc.name)
		if err != nil || got != tc.want {
			t.Fatalf("parsePlainVP9TemporalLayeringMode(%q) = %d, %v; want %d, nil",
				tc.name, got, err, tc.want)
		}
	}
	if _, err := parsePlainVP9TemporalLayeringMode("bogus"); err == nil {
		t.Fatal("parsePlainVP9TemporalLayeringMode(bogus) succeeded")
	}
}

func TestPlainVP9TelemetryResultPresentsOneLayer(t *testing.T) {
	result := govpx.VP9EncodeResult{
		Data:                 []byte{0x82, 0x49},
		KeyFrame:             true,
		ShowFrame:            true,
		SizeBytes:            2,
		TemporalLayerID:      0,
		TemporalLayerCount:   3,
		TemporalLayeringMode: temporalLayerMode,
	}

	got := plainVP9TelemetryResult(result)
	if got.LayerCount != 1 || got.SizeBytes != result.SizeBytes ||
		len(got.Data) != len(result.Data) {
		t.Fatalf("plain telemetry result = layers:%d size:%d data:%d, want one layer size %d",
			got.LayerCount, got.SizeBytes, len(got.Data), result.SizeBytes)
	}
	layer := got.Layers[0]
	if layer.SpatialLayerID != 0 || layer.SpatialLayerCount != 1 {
		t.Fatalf("plain telemetry layer spatial = %d/%d, want 0/1",
			layer.SpatialLayerID, layer.SpatialLayerCount)
	}
	if !layer.ScalabilityStructurePresent ||
		layer.SpatialScalabilityStructure.SpatialLayerCount != 1 ||
		layer.SpatialScalabilityStructure.Width[0] != plainVP9Width ||
		layer.SpatialScalabilityStructure.Height[0] != plainVP9Height {
		t.Fatalf("plain telemetry SS = %+v, want one %dx%d layer",
			layer.SpatialScalabilityStructure, plainVP9Width, plainVP9Height)
	}
}

func sdpHasRTCPFeedbackForTest(sdp string, feedback string) bool {
	want := "a=rtcp-fb:"
	feedback = strings.ToLower(strings.TrimSpace(feedback))
	for _, raw := range strings.Split(sdp, "\n") {
		line := strings.ToLower(strings.TrimSpace(raw))
		if !strings.HasPrefix(line, want) {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, want))
		if len(fields) >= 2 && strings.Join(fields[1:], " ") == feedback {
			return true
		}
	}
	return false
}

func sdpExtmapIDForTest(t *testing.T, sdpText string, uri string) uint8 {
	t.Helper()
	uri = strings.TrimSpace(uri)
	for _, raw := range strings.Split(sdpText, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "a=extmap:") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "a=extmap:"))
		if len(fields) < 2 || fields[1] != uri {
			continue
		}
		idField := strings.SplitN(fields[0], "/", 2)[0]
		id, err := strconv.Atoi(idField)
		if err != nil || id <= 0 || id > 255 {
			t.Fatalf("invalid extmap id %q for %s in SDP:\n%s",
				fields[0], uri, sdpText)
		}
		return uint8(id)
	}
	t.Fatalf("answer SDP missing extmap for %s:\n%s", uri, sdpText)
	return 0
}

func assertRTPAccessUnitHasHeaderExtensionForTest(
	t *testing.T,
	label string,
	packets []*rtp.Packet,
	extID uint8,
) {
	t.Helper()
	if extID == 0 {
		t.Fatalf("%s extension id is zero", label)
	}
	for i, packet := range packets {
		if packet == nil {
			t.Fatalf("%s packet %d is nil", label, i)
		}
		if got := packet.GetExtension(extID); len(got) == 0 {
			t.Fatalf("%s packet %d missing RTP header extension %d",
				label, i, extID)
		}
	}
}

func TestIndexHTMLExposesBrowserRTCStatsForFreezeDiagnosis(t *testing.T) {
	for _, want := range []string{
		"pc.getStats()",
		"framesDecoded",
		"framesDropped",
		"packetsLost",
		"packetsReceived",
		"freezeCount",
		"totalFreezesDuration",
		"pauseCount",
		"totalPausesDuration",
		"nackCount",
		"pliCount",
		"firCount",
		"maybeRequestReceiverRepair",
		"if(paused)",
		"receiverRepairSuppressedUntilDecoded",
		"receiverRepairSuppressUntil",
		"maybeBackoffReceiverSpatialCap",
		"receiver-stall",
		"receiverRepairStreak",
		"RECEIVER_REPAIR_COOLDOWN_MS",
		"RECEIVER_REPAIR_CAP_BACKOFF_AFTER",
		"receiverSpatialCap",
		"rx cap",
		"rx freezes",
		"rx nack",
		"rx pli",
		"rx fir",
		"rx repair",
		"senderForcedKeyCount",
		"forced keys",
		"pkt recoveries",
		"withheld AUs",
		"withheld_aus",
		"partial writes",
		"partial_write_aus",
		"enc ms",
		"encode fails",
		"encoded drops",
	} {
		if !strings.Contains(indexHTML, want) {
			t.Fatalf("indexHTML missing %q", want)
		}
	}
}

func TestReadmeDocumentsStatefulVP9WebRTCPacketizer(t *testing.T) {
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	text := string(raw)
	for _, want := range []string{
		"govpx.VP9WebRTCPacketizer",
		"PacketizeWebRTCNonFlexibleInto",
		"PacketizeSpatialSVCWebRTCNonFlexibleInto",
		"non-flexible VP9 RTP descriptors",
		"node browser_smoke.mjs",
		"--repeat 3",
		"--soak-ms 30000 --sample-ms 5000",
		"--min-decoded-delta 100 --min-video-time-ratio 0.9 --max-rx-repair-requests 0",
		"--max-rx-nack-delta 0 --max-rx-pli-delta 0 --max-rx-fir-delta 0",
		"--max-sender-failed-encode-aus 0 --max-sender-failed-encoded-aus 0",
		"--min-active-layers 3 --min-ending-active-layers 3",
		"--require-threaded-top-layer",
		"--server-plain-vp9",
		"plain single-spatial/single-temporal VP9 WebRTC path",
		"-plain-vp9-temporal",
		"--server-plain-vp9-temporal",
		"plain single-spatial/three-temporal-layer VP9 WebRTC path",
		"no-inter-layer-prediction",
		"--repeat 2 --cpu-burners 12 --server-fps 25",
		"--server-plain-vp9-temporal --control-churn --cpu-burners 12 --server-fps 25",
		"--min-active-layers 1 --min-ending-active-layers 1",
		"--control-churn",
		"sender forced-key event",
		"--max-rx-dropped-delta 1",
		"--max-rx-pli-delta 1",
		"--tuning-churn",
		"screen-content mode changes force a keyframe boundary",
		"--pause-resume --pause-ms 1500",
		"Pause/resume is gated as a lifecycle recovery path",
		"--receiver-stall-probe",
		"Receiver-side clean-stall recovery is gated",
		"--max-rx-repair-requests 1",
		"--local-withhold --local-withhold-count 2",
		"App-local no-loss withhold is gated as a sender recovery path",
		"--local-withhold --local-withhold-count 2 --cpu-burners 12 --server-fps 25",
		"app-local no-loss recovery under scheduler contention",
		"--local-partial-write --local-partial-write-count 2",
		"App-local partial RTP write is gated as a sender recovery path",
		"--control-churn --cpu-burners 12 --server-fps 25",
		"--min-video-time-ratio 0.8",
		"--clients 2",
		"--min-decoded-delta 80 --min-video-time-ratio 0.85",
		"simultaneous receiver/encoder sessions",
		"node production_gate.mjs",
		"root VP9 realtime packetizer/threading checks",
		"libwebrtc-style VP9 ref-finder simulations",
		"VP9_WEBRTC_GATE_MAX_ACCESS_UNIT_MS",
		"VP9_WEBRTC_GATE_MAX_SCHEDULE_LAG_MS",
		"node stress_gate.mjs",
		"VP9_WEBRTC_STRESS_LOADED_SOAK_MS",
		"VP9_WEBRTC_STRESS_MAX_ACCESS_UNIT_MS",
		"VP9_WEBRTC_STRESS_MAX_SCHEDULE_LAG_MS",
		"partial RTP-write recovery",
		"hostile-load stress gate",
		"access-unit or schedule-lag latency budget",
		"the full",
		"production gate and hostile-load stress gate both enforce those budgets",
		"root VP9 WebRTC packetizer, spatial-SVC",
		"browser reference-state stalls fail before manual testing",
		"zero-CPU libvpx/vpxenc speed oracle",
		"threaded libvpx/vpxenc tile oracle",
		"VP9 WebRTC pre-encode-drop libvpx/vpxdec oracle",
		"libvpx/vpxdec example oracle subset",
		"threaded top-layer",
		"browser-native NACK/PLI/FIR",
		"sender-side encode, packetization, or",
		"decoded frames and video time advance",
		"each `--sample-ms` interval",
		"active spatial-layer",
		"MarkEncodedAccessUnitUnsent",
		"MarkAccessUnitUnsent",
		"app-local gap",
		"longer CPU-contention soaks",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("README.md missing %q", want)
		}
	}
	if strings.Contains(text,
		"VP9SpatialSVCEncodeResult.PacketizeWebRTCRTPInto") {
		t.Fatal("README.md still points the demo at the stateless VP9 SVC WebRTC packetizer")
	}
}

func TestBrowserSmokeEnforcesVP9WebRTCBudgets(t *testing.T) {
	raw, err := os.ReadFile("browser_smoke.mjs")
	if err != nil {
		t.Fatalf("read browser_smoke.mjs: %v", err)
	}
	text := string(raw)
	for _, want := range []string{
		`maxRxDroppedDelta: numberFlag("--max-rx-dropped-delta", 0, { min: 0 })`,
		`maxRxNackDelta: numberFlag("--max-rx-nack-delta", 0, { min: 0 })`,
		`maxRxPliDelta: numberFlag("--max-rx-pli-delta", 0, { min: 0 })`,
		`maxRxFirDelta: numberFlag("--max-rx-fir-delta", 0, { min: 0 })`,
		`tuningChurn: booleanFlag("--tuning-churn")`,
		`maxAccessUnitMs: optionalNumberFlag("--max-access-unit-ms")`,
		`maxScheduleLagMs: optionalNumberFlag("--max-schedule-lag-ms")`,
		"nextControlAction(opts, i)",
		"controlChurnAction(opts, sampleIndex)",
		"plainVP9ControlChurnAction",
		"plainVP9ControlChurnAction(sampleIndex, 1)",
		`if (opts.serverPlainVP9 || opts.serverPlainVP9Temporal)`,
		"tuningChurnAction",
		`{ type: "bitrate", kbps: 1200 }`,
		`{ type: "screen", mode: 1, requiresForcedKey: true }`,
		`return {type: "bitrate", kbps: Number(input.value)}`,
		`return {type: "screen", mode: action.mode}`,
		"second.targetKbps !== opts.controlAction.kbps",
		"second.screenMode !== opts.controlAction.mode",
		`pauseResume: booleanFlag("--pause-resume")`,
		`pauseMs: numberFlag("--pause-ms", 1500, { min: 0 })`,
		`receiverStallProbe: booleanFlag("--receiver-stall-probe")`,
		`serverPlainVP9: booleanFlag("--server-plain-vp9")`,
		`serverPlainVP9Temporal: booleanFlag("--server-plain-vp9-temporal")`,
		`serverPlainVP9TemporalMode: stringFlag("--server-plain-vp9-temporal-mode", "default")`,
		`if (opts.serverPlainVP9 || opts.serverPlainVP9Temporal) serverArgs.push("-plain-vp9")`,
		`if (opts.serverPlainVP9Temporal) serverArgs.push("-plain-vp9-temporal")`,
		`serverArgs.push("-plain-vp9-temporal-mode", opts.serverPlainVP9TemporalMode)`,
		`localWithhold: booleanFlag("--local-withhold")`,
		`localWithholdCount: integerFlag("--local-withhold-count", 1, { min: 1, max: 3 })`,
		`localPartialWrite: booleanFlag("--local-partial-write")`,
		`localPartialWriteCount: integerFlag("--local-partial-write-count", 1, { min: 1, max: 3 })`,
		`${name} must be <= ${opts.max}`,
		"exercisePauseResume(cdp, clients, initialByClient, opts.timeoutMs, opts.pauseMs)",
		"exerciseReceiverStallProbe(cdp, clients, firstByClient, opts.timeoutMs)",
		"exerciseLocalWithhold(cdp, clients, firstByClient, opts.timeoutMs, opts.localWithholdCount)",
		"exerciseLocalPartialWrite(cdp, clients, firstByClient, opts.timeoutMs, opts.localPartialWriteCount)",
		`applyControlAction(cdp, client.sessionId, { type: "pause", paused: true })`,
		`applyControlAction(cdp, client.sessionId, { type: "pause", paused: false })`,
		`applyControlAction(cdp, client.sessionId, { type: "withhold", count })`,
		`applyControlAction(cdp, client.sessionId, { type: "partial-write", count })`,
		"waitForPauseResumeRecovery",
		"waitForReceiverStallProbeRecovery",
		"triggerReceiverStallProbe",
		"receiver stall probe did not emit repair controls",
		"receiver stall probe did not produce clean forced-key recovery",
		"waitForLocalWithholdRecovery",
		"waitForLocalPartialWriteRecovery",
		"recoveredByClient",
		`return {type: "pause", paused}`,
		"opts.pauseResume &&",
		"forcedKeysAfterResume",
		"decodedAfterResume",
		"pause/resume did not produce clean forced-key decode recovery",
		"local withhold did not produce clean packetizer recovery",
		"local partial write did not produce clean packetizer recovery",
		"senderWithheldAUs",
		"maxSenderWithheldAUs",
		"senderPartialWriteAUs",
		"maxSenderPartialWriteAUs",
		"maxSenderFailedEncodeAUs: opts.maxSenderFailedEncodeAUs",
		"maxSenderFailedEncodedAUs: opts.maxSenderFailedEncodedAUs",
		"maxRxDroppedDelta: opts.maxRxDroppedDelta",
		"delta.rxDropped !== null && delta.rxDropped > opts.maxRxDroppedDelta",
		"rxDropped changed by",
		"opts.summary.maxSenderFailedEncodeAUs > opts.maxSenderFailedEncodeAUs",
		"opts.summary.maxSenderFailedEncodedAUs > opts.maxSenderFailedEncodedAUs",
		"opts.summary.maxAccessUnitMs > opts.maxAccessUnitMs",
		"opts.summary.maxScheduleLagMs > opts.maxScheduleLagMs",
		"rxFreezeDuration",
		"rxPauseCount",
		"rxPauseDuration",
		`["rxFreezeDuration", "rxPauseCount", "rxPauseDuration"]`,
		"advanced during clean smoke",
		`["rxNackCount", opts.maxRxNackDelta, "receiver NACK"]`,
		`["rxPliCount", opts.maxRxPliDelta, "receiver PLI"]`,
		`["rxFirCount", opts.maxRxFirDelta, "receiver FIR"]`,
		"delta[key] !== null && delta[key] > max",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("browser_smoke.mjs missing %q", want)
		}
	}
	if count := strings.Count(text,
		"maxSenderFailedEncodeAUs: opts.maxSenderFailedEncodeAUs"); count < 2 {
		t.Fatalf("sender failed encode budget only wired %d time(s), want parse output and assertion", count)
	}
	if count := strings.Count(text,
		"maxSenderFailedEncodedAUs: opts.maxSenderFailedEncodedAUs"); count < 2 {
		t.Fatalf("sender failed encoded budget only wired %d time(s), want parse output and assertion", count)
	}
}

func TestProductionGateReportsVP9BrowserStallBudgets(t *testing.T) {
	raw, err := os.ReadFile("production_gate.mjs")
	if err != nil {
		t.Fatalf("read production_gate.mjs: %v", err)
	}
	text := string(raw)
	for _, want := range []string{
		"TestPlainVP9WebRTC.*Vpxdec",
		"focusedGoPattern",
		"rootGoPattern",
		"TestPlainVP9FlexiblePacketizerHandlesForcedKeyChurn",
		"TestNewVP9EncoderPromotesZeroCPUUsedByDeadline",
		"TestVP9EncodeResultPacketizeWebRTCRTP",
		"TestVP9WebRTCPacketizer.*",
		"ExampleVP9WebRTCPacketizer_PacketizeWebRTCNonFlexible",
		"TestVP9SpatialSVCEncodeResultPacketizeWebRTCRTP.*",
		"TestVP9RowMT.*",
		"TestPionVP9SamplePayloaderOmitsGovpxSVCWebRTCMetadata",
		"TestPlainVP9WebRTCPacketizerPassesLibwebrtcVP9RefFinder",
		"TestPlainVP9PacketizedCBRDropsPassLibwebrtcVP9RefFinder",
		"TestVP9WebRTCPacketizerSVCNonFlexiblePassesLibwebrtcVP9RefFinder",
		"TestVP9WebRTCPacketizerSVCDefaultKeyIntervalPassesLibwebrtcVP9RefFinder",
		"TestVP9WebRTCPacketizerSVCNonFlexibleRecoveryAfterKeyIntervalUnsentAccessUnitPassesLibwebrtcVP9RefFinder",
		"TestVP9WebRTCPacketizerSVCNonFlexibleRecoveryAfterPacketizedUnsentAccessUnitPassesLibwebrtcVP9RefFinder",
		"TestVP9WebRTCPacketizerSVCNonFlexibleRecoveryAfterPartialWriteAccessUnitPassesLibwebrtcVP9RefFinder",
		"TestConsumeLocal(Withhold|PartialWrite)AccessUnit",
		"TestPartialWriteRTPWriterFailsAfterPrefix",
		"TestWebRTCPacketizedSVCPassesRefFinderAcrossTL0Wrap",
		"browser-receiver-stall-probe",
		"browser-plain-vp9",
		"browser-plain-vp9-control-churn",
		"browser-plain-vp9-temporal",
		"browser-plain-vp9-temporal-control-churn",
		"browser-plain-vp9-temporal-loaded",
		"browser-plain-vp9-temporal-loaded-control-churn",
		"--server-plain-vp9",
		"--server-plain-vp9-temporal",
		`"--min-decoded-delta", "70"`,
		`"--min-video-time-ratio", "0.8"`,
		`"--max-rx-dropped-delta", "1"`,
		`"--max-rx-pli-delta", "1"`,
		"--receiver-stall-probe",
		`"--max-rx-repair-requests", "1"`,
		"browser-local-withhold",
		"browser-loaded-local-withhold",
		"--local-withhold",
		`"--local-withhold-count", "2"`,
		"browser-local-partial-write",
		"--local-partial-write",
		`"--local-partial-write-count", "2"`,
		`"--max-sender-failed-encoded-aus", "2"`,
		`"--cpu-burners", "12"`,
		`VP9_WEBRTC_GATE_MAX_ACCESS_UNIT_MS`,
		`VP9_WEBRTC_GATE_MAX_SCHEDULE_LAG_MS`,
		`"--max-access-unit-ms", String(maxAccessUnitMs)`,
		`"--max-schedule-lag-ms", String(maxScheduleLagMs)`,
		"...browserLatencyBudgets",
		"maxSenderWithheldAUs",
		"maxSenderPartialWriteAUs",
		"rootOraclePattern",
		"libvpx-root-oracle",
		"TestVP9EncoderVpxencOracleRealtimeZeroCPUUsesSpeed8",
		"TestVP9OracleThreadedTileEncodingMatchesLibvpx",
		"TestVP9WebRTCPreEncodeDropPacketizedStreamDecodesWithVpxdec",
		`GOVPX_WITH_ORACLE: "1"`,
		"requiresOracle: true",
		"assertNoOracleSkips",
		`line.startsWith("--- SKIP:")`,
		"freezeDuration: aggregate.freezeDuration",
		"pauses: aggregate.pauses",
		"pauseDuration: aggregate.pauseDuration",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("production_gate.mjs missing %q", want)
		}
	}
	if count := strings.Count(text, "...browserLatencyBudgets"); count < 10 {
		t.Fatalf("production gate browser latency budgets wired %d time(s), want every browser smoke step", count)
	}
}

func TestStressGateReportsVP9HostileSoakBudgets(t *testing.T) {
	raw, err := os.ReadFile("stress_gate.mjs")
	if err != nil {
		t.Fatalf("read stress_gate.mjs: %v", err)
	}
	text := string(raw)
	for _, want := range []string{
		"browser-loaded-long-soak",
		"browser-loaded-control-soak",
		"browser-plain-vp9-temporal-loaded-control-soak",
		"browser-loaded-withhold-soak",
		"browser-loaded-partial-write-soak",
		"plainTemporalRecoveryLoadedBudgets",
		"--server-plain-vp9-temporal",
		"--cpu-burners",
		"--server-fps",
		"--local-withhold-count",
		"--local-partial-write-count",
		"partialWriteLoadedBudgets",
		`VP9_WEBRTC_STRESS_LOADED_SOAK_MS`,
		`VP9_WEBRTC_STRESS_CONTROL_SOAK_MS`,
		`VP9_WEBRTC_STRESS_WITHHOLD_SOAK_MS`,
		`VP9_WEBRTC_STRESS_MAX_ACCESS_UNIT_MS`,
		`VP9_WEBRTC_STRESS_MAX_SCHEDULE_LAG_MS`,
		`VP9_WEBRTC_STRESS_CPU_BURNERS`,
		`VP9_WEBRTC_STRESS_SERVER_FPS`,
		`VP9_WEBRTC_STRESS_REPEAT`,
		`"--max-access-unit-ms"`,
		`"--max-schedule-lag-ms"`,
		"libvpx-vpxdec-recovery-oracle",
		"TestVP9WebRTCPacketizerSVCNonFlexibleRecoveryAfter(ConsecutivePacketizedUnsentAccessUnits|PartialWriteAccessUnit)DecodesWithVpxdec",
		`GOVPX_WITH_ORACLE: "1"`,
		"requiresOracle: true",
		"assertNoOracleSkips",
		`line.startsWith("--- SKIP:")`,
		"maxSenderFailedEncodeAUs",
		"maxSenderFailedEncodedAUs",
		"maxSenderPartialWriteAUs",
		"maxScheduleLagMs",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("stress_gate.mjs missing %q", want)
		}
	}
}

func TestSDPNegotiatesVP9Profile0(t *testing.T) {
	tests := []struct {
		name string
		sdp  string
		want bool
	}{
		{
			name: "vp9 profile zero",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0",
			}, "\r\n"),
			want: true,
		},
		{
			name: "vp9 profile zero among fmtp params",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 x-google-start-bitrate=800; profile-id = 0 ; max-fr=30",
			}, "\r\n"),
			want: true,
		},
		{
			name: "vp9 profile zero after audio section",
			sdp: strings.Join([]string{
				"m=audio 9 UDP/TLS/RTP/SAVPF 111",
				"a=rtpmap:111 opus/48000/2",
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0",
			}, "\r\n"),
			want: true,
		},
		{
			name: "vp9 profile zero implied by missing fmtp",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
			}, "\r\n"),
			want: true,
		},
		{
			name: "vp9 profile zero implied by fmtp without profile id",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 max-fr=30; max-fs=920",
			}, "\r\n"),
			want: true,
		},
		{
			name: "vp9 profile two",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 100",
				"a=rtpmap:100 VP9/90000",
				"a=fmtp:100 profile-id=2",
			}, "\r\n"),
		},
		{
			name: "profile zero without vp9 codec",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 96",
				"a=rtpmap:96 VP8/90000",
				"a=fmtp:96 profile-id=0",
			}, "\r\n"),
		},
		{
			name: "profile zero belongs to different payload",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 96 100",
				"a=rtpmap:96 VP8/90000",
				"a=fmtp:96 profile-id=0",
				"a=rtpmap:100 VP9/90000",
				"a=fmtp:100 profile-id=2",
			}, "\r\n"),
		},
		{
			name: "lookalike fmtp key does not override implied profile zero",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 x-profile-id=0",
			}, "\r\n"),
			want: true,
		},
		{
			name: "lookalike fmtp value is rejected",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=00",
			}, "\r\n"),
		},
		{
			name: "profile zero suffix is rejected",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0foo",
			}, "\r\n"),
		},
		{
			name: "vp9 profile zero not listed on video m line",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 100",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0",
			}, "\r\n"),
		},
		{
			name: "vp9 profile zero in audio section",
			sdp: strings.Join([]string{
				"m=audio 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0",
			}, "\r\n"),
		},
		{
			name: "disabled video section",
			sdp: strings.Join([]string{
				"m=video 0 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0",
			}, "\r\n"),
		},
		{
			name: "inactive video section",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=inactive",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0",
			}, "\r\n"),
		},
		{
			name: "stale payload from previous video section",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP8/90000",
				"m=video 9 UDP/TLS/RTP/SAVPF 100",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0",
			}, "\r\n"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := govpx.VP9SDPNegotiatesProfile0(tc.sdp); got != tc.want {
				t.Fatalf("VP9SDPNegotiatesProfile0 = %t, want %t",
					got, tc.want)
			}
		})
	}
}

func TestSDPOffersVP9Profile0Receive(t *testing.T) {
	tests := []struct {
		name      string
		direction string
		want      bool
	}{
		{name: "default sendrecv", want: true},
		{name: "media sendrecv", direction: "a=sendrecv", want: true},
		{name: "media recvonly", direction: "a=recvonly", want: true},
		{name: "media sendonly", direction: "a=sendonly"},
		{name: "media inactive", direction: "a=inactive"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lines := []string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0",
			}
			if tc.direction != "" {
				lines = append(lines[:1], append([]string{tc.direction}, lines[1:]...)...)
			}
			if got := govpx.VP9SDPOffersProfile0Receive(strings.Join(lines, "\r\n")); got != tc.want {
				t.Fatalf("VP9SDPOffersProfile0Receive = %t, want %t",
					got, tc.want)
			}
		})
	}
}

func TestSDPOffersVP9Profile0ReceiveFrame(t *testing.T) {
	tests := []struct {
		name string
		sdp  string
		want bool
	}{
		{
			name: "no fmtp is unconstrained profile zero",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP9/90000",
			}, "\r\n"),
			want: true,
		},
		{
			name: "receiver caps allow frame",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0; max-fr=30; max-fs=920",
			}, "\r\n"),
			want: true,
		},
		{
			name: "receiver caps infer profile zero",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 max-fr=30; max-fs=920",
			}, "\r\n"),
			want: true,
		},
		{
			name: "max-fr too low",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0; max-fr=29; max-fs=920",
			}, "\r\n"),
		},
		{
			name: "max-fs too low",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0; max-fr=30; max-fs=919",
			}, "\r\n"),
		},
		{
			name: "profile two rejected",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=2; max-fr=30; max-fs=920",
			}, "\r\n"),
		},
		{
			name: "invalid receiver cap rejected",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=recvonly",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0; max-fr=30; max-fs=wide",
			}, "\r\n"),
		},
		{
			name: "one vp9 payload can receive frame",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98 100",
				"a=recvonly",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0; max-fr=30; max-fs=919",
				"a=rtpmap:100 VP9/90000",
				"a=fmtp:100 profile-id=0; max-fr=30; max-fs=920",
			}, "\r\n"),
			want: true,
		},
		{
			name: "sendonly rejected",
			sdp: strings.Join([]string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=sendonly",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0; max-fr=30; max-fs=920",
			}, "\r\n"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := govpx.VP9SDPOffersProfile0ReceiveFrame(tc.sdp, 640, 360, 30)
			if got != tc.want {
				t.Fatalf("VP9SDPOffersProfile0ReceiveFrame = %t, want %t",
					got, tc.want)
			}
		})
	}
}

func TestSDPAnswersVP9Profile0Send(t *testing.T) {
	tests := []struct {
		name      string
		direction string
		want      bool
	}{
		{name: "default sendrecv", want: true},
		{name: "media sendrecv", direction: "a=sendrecv", want: true},
		{name: "media sendonly", direction: "a=sendonly", want: true},
		{name: "media recvonly", direction: "a=recvonly"},
		{name: "media inactive", direction: "a=inactive"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lines := []string{
				"m=video 9 UDP/TLS/RTP/SAVPF 98",
				"a=rtpmap:98 VP9/90000",
				"a=fmtp:98 profile-id=0",
			}
			if tc.direction != "" {
				lines = append(lines[:1], append([]string{tc.direction}, lines[1:]...)...)
			}
			if got := govpx.VP9SDPAnswersProfile0Send(strings.Join(lines, "\r\n")); got != tc.want {
				t.Fatalf("VP9SDPAnswersProfile0Send = %t, want %t",
					got, tc.want)
			}
		})
	}
}

func TestPickThreadsEnablesTileWorkersForRealtimeLayers(t *testing.T) {
	tests := []struct {
		name        string
		width       int
		height      int
		wantAtLeast int
		wantAtMost  int
	}{
		{"base-layer-stays-single-threaded", 160, 90, 1, 1},
		{"middle-layer-stays-within-vp9-tile-limit", 320, 180, 1, 1},
		{"top-layer-uses-two-columns-when-available", 640, 360, expectedThreads(640, 360), 2},
		{"wide-layer-can-use-four-columns", 1280, 720, expectedThreads(1280, 720), 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pickThreads(tc.width, tc.height)
			if got < tc.wantAtLeast || got > tc.wantAtMost {
				t.Fatalf("pickThreads(%d, %d) = %d, want in [%d,%d]",
					tc.width, tc.height, got, tc.wantAtLeast, tc.wantAtMost)
			}
			if got > runtime.NumCPU() {
				t.Fatalf("pickThreads(%d, %d) = %d exceeds NumCPU=%d",
					tc.width, tc.height, got, runtime.NumCPU())
			}
			if got > maxVP9TileColumns(tc.width) {
				t.Fatalf("pickThreads(%d, %d) = %d exceeds legal VP9 tile columns=%d",
					tc.width, tc.height, got, maxVP9TileColumns(tc.width))
			}
		})
	}
}

func TestSVCLayerOptionsKeepRowMTOffUntilRowWorkersAreLive(t *testing.T) {
	tests := []struct {
		name        string
		width       int
		height      int
		wantCPUUsed int8
	}{
		{"base-layer", 160, 90, 8},
		{"middle-layer", 320, 180, 8},
		{"top-layer", 640, 360, 9},
		{"wide-layer", 1280, 720, 9},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := newSVCLayerOptions(tc.width, tc.height, defaultFPS, 700)
			wantThreads := pickThreads(tc.width, tc.height)
			if opts.Threads != wantThreads {
				t.Fatalf("Threads = %d, want %d", opts.Threads, wantThreads)
			}
			if opts.CpuUsed != tc.wantCPUUsed {
				t.Fatalf("CpuUsed = %d, want %d", opts.CpuUsed, tc.wantCPUUsed)
			}
			if opts.RowMT {
				t.Fatalf("RowMT = true for %d tile threads, want false until VP9 row workers are on the production path",
					wantThreads)
			}
			if !opts.ErrorResilient ||
				!opts.FrameParallelDecodingSet ||
				!opts.FrameParallelDecoding {
				t.Fatalf("VP9 resilience flags = err:%t fp-set:%t fp:%t, want true/true/true",
					opts.ErrorResilient,
					opts.FrameParallelDecodingSet,
					opts.FrameParallelDecoding)
			}
		})
	}
}

func TestWebRTCPacketizedSVCDecodeContinuityAndCapRecovery(t *testing.T) {
	svc, err := newSVCEncoder(demoConfig{
		FPS:         defaultFPS,
		BitrateKbps: defaultBitrateKbps,
	})
	if err != nil {
		t.Fatalf("newSVCEncoder: %v", err)
	}
	defer svc.Close()

	var decoders [spatialLayerCount]*govpx.VP9Decoder
	for layer := 0; layer < spatialLayerCount; layer++ {
		decoders[layer], err = govpx.NewVP9Decoder(govpx.VP9DecoderOptions{
			SVCSpatialLayerSet: true,
			SVCSpatialLayer:    uint8(layer),
		})
		if err != nil {
			t.Fatalf("NewVP9Decoder layer %d: %v", layer, err)
		}
		defer decoders[layer].Close()
	}

	imgs := make([]*image.YCbCr, spatialLayerCount)
	for i := range imgs {
		imgs[i] = image.NewYCbCr(image.Rect(0, 0, layerDims[i][0], layerDims[i][1]),
			image.YCbCrSubsampleRatio420)
	}
	dst := make([]byte, superframeBudget())
	caps := []int{3, 3, 2, 2, 1, 3, 3, 2, 3}
	pictureID := uint16(0x7ffc)
	lastCap := caps[0]
	for frame, cap := range caps {
		if frame > 0 && cap != lastCap {
			forceKeyAll(svc)
		}
		drawScene(imgs, frame)
		result, err := svc.EncodeActiveLayersIntoWithResult(imgs, dst, cap)
		if err != nil {
			t.Fatalf("EncodeActiveLayersIntoWithResult frame %d cap %d: %v",
				frame, cap, err)
		}
		if int(result.LayerCount) != cap {
			t.Fatalf("frame %d active layer count = %d, want %d",
				frame, result.LayerCount, cap)
		}
		if frame > 0 && cap != lastCap {
			base := result.Layers[0]
			if !base.KeyFrame || base.InterPicturePredicted {
				t.Fatalf("frame %d cap %d->%d base = key:%t inter:%t, want key/non-predicted",
					frame, lastCap, cap, base.KeyFrame,
					base.InterPicturePredicted)
			}
			for spatial := 1; spatial < cap; spatial++ {
				if result.Layers[spatial].KeyFrame ||
					!result.Layers[spatial].ShowFrame {
					t.Fatalf("frame %d cap %d->%d layer %d = key:%t show:%t, want visible inter-layer refresh",
						frame, lastCap, cap, spatial,
						result.Layers[spatial].KeyFrame,
						result.Layers[spatial].ShowFrame)
				}
			}
			for spatial := cap; spatial < spatialLayerCount; spatial++ {
				if result.Layers[spatial].Data != nil ||
					result.Layers[spatial].SizeBytes != 0 {
					t.Fatalf("frame %d inactive layer %d result = %+v, want zero",
						frame, spatial, result.Layers[spatial])
				}
			}
		}
		payloads := packetizeWebRTCSVCResultForTest(t, result, pictureID, 500)
		packet := reassembleWebRTCSVCResultForTest(t, result, payloads, pictureID)
		for layer := 0; layer < cap; layer++ {
			assertWebRTCSVCDecoderOutputForTest(t, decoders[layer],
				packet, frame, layer, layerDims[layer][0], layerDims[layer][1])
		}
		lastCap = cap
		pictureID = govpx.NextVP9RTPPictureID(pictureID)
	}
}

func TestSVCEncoderEmitsThreadedTopLayerTileLayout(t *testing.T) {
	result := encodeOneSVCResultForTest(t)

	top := result.Layers[spatialLayerCount-1]
	info, err := govpx.PeekVP9StreamInfo(top.Data)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo top layer: %v", err)
	}
	topWidth, topHeight := layerDims[spatialLayerCount-1][0], layerDims[spatialLayerCount-1][1]
	wantLog2Cols := expectedTileLog2Cols(pickThreads(topWidth, topHeight))
	if !info.TileInfoAvailable || info.TileLog2Cols != wantLog2Cols || info.TileLog2Rows != 0 {
		t.Fatalf("top-layer tile info = available:%v log2:%dx%d, want available %dx0",
			info.TileInfoAvailable, info.TileLog2Cols, info.TileLog2Rows, wantLog2Cols)
	}
}

func TestSVCEncoderUsesThreeTemporalLayers(t *testing.T) {
	results := encodeSVCResultsForTest(t, 5)
	wantTemporalID := []int{0, 2, 1, 2, 0}
	wantTL0 := []uint8{0, 0, 0, 0, 1}

	for frame, result := range results {
		if result.LayerCount != spatialLayerCount {
			t.Fatalf("frame %d layer count = %d, want %d",
				frame, result.LayerCount, spatialLayerCount)
		}
		base := result.Layers[0]
		for spatial := 0; spatial < spatialLayerCount; spatial++ {
			layer := result.Layers[spatial]
			if layer.TemporalLayerCount != 3 ||
				layer.TemporalLayerID != wantTemporalID[frame] ||
				layer.TL0PICIDX != wantTL0[frame] {
				t.Fatalf("frame %d layer %d temporal = id:%d count:%d tl0:%d, want id:%d count:3 tl0:%d",
					frame, spatial, layer.TemporalLayerID, layer.TemporalLayerCount,
					layer.TL0PICIDX, wantTemporalID[frame], wantTL0[frame])
			}
			if spatial > 0 &&
				(layer.TemporalLayerID != base.TemporalLayerID ||
					layer.TL0PICIDX != base.TL0PICIDX) {
				t.Fatalf("frame %d layer %d temporal metadata drifted from base: got id:%d tl0:%d, base id:%d tl0:%d",
					frame, spatial, layer.TemporalLayerID, layer.TL0PICIDX,
					base.TemporalLayerID, base.TL0PICIDX)
			}
		}
	}

	desc := results[0].Layers[0].RTPPayloadDescriptor()
	if !desc.ScalabilityStructurePresent ||
		desc.ScalabilityStructure.SpatialLayerCount != spatialLayerCount ||
		!desc.ScalabilityStructure.PictureGroupPresent ||
		len(desc.ScalabilityStructure.PictureGroups) != 4 {
		t.Fatalf("base RTP SS = present:%v spatial:%d groups:%v/%d, want %d spatial layers and 4 temporal picture groups",
			desc.ScalabilityStructurePresent,
			desc.ScalabilityStructure.SpatialLayerCount,
			desc.ScalabilityStructure.PictureGroupPresent,
			len(desc.ScalabilityStructure.PictureGroups),
			spatialLayerCount)
	}
}

func TestForceKeyAllRefreshesEverySpatialLayer(t *testing.T) {
	svc, err := newSVCEncoder(demoConfig{
		FPS:         defaultFPS,
		BitrateKbps: defaultBitrateKbps,
	})
	if err != nil {
		t.Fatalf("newSVCEncoder: %v", err)
	}
	defer svc.Close()

	imgs := make([]*image.YCbCr, spatialLayerCount)
	for i := range imgs {
		imgs[i] = image.NewYCbCr(image.Rect(0, 0, layerDims[i][0], layerDims[i][1]),
			image.YCbCrSubsampleRatio420)
	}
	dst := make([]byte, superframeBudget())
	for frame := 0; frame < 2; frame++ {
		drawScene(imgs, frame)
		if _, err := svc.EncodeIntoWithResult(imgs, dst); err != nil {
			t.Fatalf("warm EncodeIntoWithResult frame %d: %v", frame, err)
		}
	}

	forceKeyAll(svc)
	if !svc.IsKeyFrameNext() {
		t.Fatal("ForceKeyFrame request was not armed")
	}
	drawScene(imgs, 2)
	result, err := svc.EncodeIntoWithResult(imgs, dst)
	if err != nil {
		t.Fatalf("forced EncodeIntoWithResult: %v", err)
	}
	base := result.Layers[0]
	if !base.KeyFrame || base.InterPicturePredicted {
		t.Fatalf("base forced result = key:%t inter-pred:%t, want key/non-predicted",
			base.KeyFrame, base.InterPicturePredicted)
	}
	for spatial := 1; spatial < spatialLayerCount; spatial++ {
		layer := result.Layers[spatial]
		if layer.KeyFrame || !layer.ShowFrame || layer.InterPicturePredicted {
			t.Fatalf("layer %d forced result = key:%t show:%t inter:%t, want visible non-predicted inter-layer refresh",
				spatial, layer.KeyFrame, layer.ShowFrame,
				layer.InterPicturePredicted)
		}
	}
	payloads := packetizeWebRTCSVCResultForTest(t, result, 0x72, 500)
	var seenStart [spatialLayerCount]bool
	for i, payload := range payloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor forced[%d]: %v", i, err)
		}
		if !desc.StartOfFrame {
			continue
		}
		if int(desc.SpatialID) >= spatialLayerCount {
			t.Fatalf("forced payload %d spatial id = %d, want < %d",
				i, desc.SpatialID, spatialLayerCount)
		}
		seenStart[desc.SpatialID] = true
		if desc.InterPicturePredicted {
			t.Fatalf("forced payload %d layer %d kept P=1; browser refresh requires P=0",
				i, desc.SpatialID)
		}
	}
	for spatial := range seenStart {
		if !seenStart[spatial] {
			t.Fatalf("forced RTP access unit missing layer %d start", spatial)
		}
	}
	if svc.IsKeyFrameNext() {
		t.Fatal("ForceKeyFrame request remained armed after encode")
	}
}

func TestPacketizeSVCResultForWebRTCAddsPictureID(t *testing.T) {
	result := encodeOneSVCResultForTest(t)
	const pictureID = uint16(0x1234)
	payloads := packetizeWebRTCSVCResultForTest(t, result, pictureID, 500)
	if len(payloads) == 0 {
		t.Fatal("PacketizeWebRTCRTPInto returned no payloads")
	}

	var sawBaseSS bool
	for i, payload := range payloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if !desc.PictureIDPresent || !desc.PictureID15Bit ||
			desc.PictureID != pictureID {
			t.Fatalf("payload %d PictureID = present:%v 15bit:%v id:%d, want %d",
				i, desc.PictureIDPresent, desc.PictureID15Bit,
				desc.PictureID, pictureID)
		}
		if got, want := payload.Marker, i == len(payloads)-1; got != want {
			t.Fatalf("payload %d marker = %v, want %v", i, got, want)
		}
		if desc.SpatialID == 0 && desc.StartOfFrame {
			sawBaseSS = desc.ScalabilityStructurePresent &&
				desc.ScalabilityStructure.SpatialLayerCount == spatialLayerCount
		}
	}
	if !sawBaseSS {
		t.Fatal("base WebRTC packet did not carry full scalability structure")
	}
	if got := govpx.NextVP9RTPPictureID(govpx.VP9RTPPictureID15BitMask); got != 0 {
		t.Fatalf("NextVP9RTPPictureID wrap = %d, want 0", got)
	}
}

func TestPacketizeSVCResultForWebRTCSignalsSSOnBaseKeyOnly(t *testing.T) {
	results := encodeSVCResultsForTest(t, 2)
	keyPayloads := packetizeWebRTCSVCResultForTest(t, results[0], 0x60, 500)
	if !webRTCSVCBaseStartHasSSForTest(t, keyPayloads) {
		t.Fatal("base key RTP frame did not signal scalability structure")
	}

	deltaPayloads := packetizeWebRTCSVCResultForTest(t, results[1], 0x61, 500)
	for i, payload := range deltaPayloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if desc.ScalabilityStructurePresent {
			t.Fatalf("delta payload %d repeated scalability structure", i)
		}
	}
}

func TestPacketizeCappedSVCResultForWebRTCSignalsActiveScalabilityStructure(t *testing.T) {
	result := encodeOneSVCResultForTest(t)
	capped := limitSVCResultForRTPForTest(t, result, 2)
	payloads := packetizeWebRTCSVCResultForTest(t, capped, 0x55, 500)

	base, _, err := govpx.ParseVP9RTPPayloadDescriptor(payloads[0].Payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor base: %v", err)
	}
	if !base.PictureIDPresent || base.PictureID != 0x55 {
		t.Fatalf("base PictureID = present:%v id:%d, want 0x55",
			base.PictureIDPresent, base.PictureID)
	}
	if !base.ScalabilityStructurePresent ||
		base.ScalabilityStructure.SpatialLayerCount != 2 {
		t.Fatalf("base SS = present:%v layers:%d, want active 2-layer structure",
			base.ScalabilityStructurePresent,
			base.ScalabilityStructure.SpatialLayerCount)
	}
	if base.ScalabilityStructure.Width[2] != 0 ||
		base.ScalabilityStructure.Height[2] != 0 {
		t.Fatalf("base SS leaked hidden layer dimensions = %dx%d",
			base.ScalabilityStructure.Width[2],
			base.ScalabilityStructure.Height[2])
	}
	for i, payload := range payloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if desc.SpatialID >= 2 {
			t.Fatalf("payload %d spatial id = %d, want capped layers < 2",
				i, desc.SpatialID)
		}
	}
}

func TestCappedSVCResultForRTPKeepsActiveScalabilityStructure(t *testing.T) {
	result := encodeOneSVCResultForTest(t)
	capped := limitSVCResultForRTPForTest(t, result, 2)
	wantSize := result.Layers[0].SizeBytes + result.Layers[1].SizeBytes
	if capped.SizeBytes != wantSize || capped.LayerCount != 2 {
		t.Fatalf("capped result accounting = size:%d layers:%d, want %d/2",
			capped.SizeBytes, capped.LayerCount, wantSize)
	}
	payloads := packetizeWebRTCSVCResultForTest(t, capped, 0x56, 500)
	if len(payloads) == 0 {
		t.Fatal("capped WebRTC packetizer returned no payloads")
	}

	base, _, err := govpx.ParseVP9RTPPayloadDescriptor(payloads[0].Payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor base: %v", err)
	}
	if !base.ScalabilityStructurePresent ||
		base.ScalabilityStructure.SpatialLayerCount != 2 {
		t.Fatalf("base SS = present:%v layers:%d, want active 2-layer structure",
			base.ScalabilityStructurePresent,
			base.ScalabilityStructure.SpatialLayerCount)
	}
	if !base.ScalabilityStructure.PictureGroupPresent ||
		len(base.ScalabilityStructure.PictureGroups) != 4 {
		t.Fatalf("base capped SS temporal groups = present:%v count:%d, want 4 groups",
			base.ScalabilityStructure.PictureGroupPresent,
			len(base.ScalabilityStructure.PictureGroups))
	}
	if base.NotRefForUpperSpatialLayer {
		t.Fatal("base descriptor unexpectedly marked not-reference-for-upper")
	}
	for spatial := 0; spatial < int(capped.LayerCount); spatial++ {
		if capped.Layers[spatial].SpatialLayerCount != capped.LayerCount {
			t.Fatalf("layer %d SpatialLayerCount = %d, want capped count %d",
				spatial, capped.Layers[spatial].SpatialLayerCount,
				capped.LayerCount)
		}
	}

	var foundEnhancement bool
	for _, payload := range payloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor enhancement scan: %v", err)
		}
		if desc.LayerIndicesPresent && desc.SpatialID == 1 && desc.StartOfFrame {
			foundEnhancement = true
			if !desc.NotRefForUpperSpatialLayer {
				t.Fatal("capped enhancement layer was not marked not-reference-for-upper")
			}
			if desc.ScalabilityStructurePresent {
				t.Fatal("enhancement layer repeated scalability structure")
			}
		}
	}
	if !foundEnhancement {
		t.Fatal("did not find capped enhancement-layer RTP frame")
	}
}

func TestCappedSVCResultForRTPSingleLayerSignalsBaseOnly(t *testing.T) {
	result := encodeOneSVCResultForTest(t)
	capped := limitSVCResultForRTPForTest(t, result, 1)
	payloads := packetizeWebRTCSVCResultForTest(t, capped, 0x57, 500)

	base, _, err := govpx.ParseVP9RTPPayloadDescriptor(payloads[0].Payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor base: %v", err)
	}
	if !base.ScalabilityStructurePresent ||
		base.ScalabilityStructure.SpatialLayerCount != 1 {
		t.Fatalf("base-only SS = present:%v layers:%d, want one active layer",
			base.ScalabilityStructurePresent,
			base.ScalabilityStructure.SpatialLayerCount)
	}
	if base.ScalabilityStructure.Width[1] != 0 ||
		base.ScalabilityStructure.Height[1] != 0 {
		t.Fatalf("base-only SS leaked hidden layer dimensions = %dx%d",
			base.ScalabilityStructure.Width[1],
			base.ScalabilityStructure.Height[1])
	}
	if !base.NotRefForUpperSpatialLayer {
		t.Fatal("base-only descriptor was not marked not-reference-for-upper")
	}
	if capped.Layers[0].SpatialLayerCount != 1 {
		t.Fatalf("base-only SpatialLayerCount = %d, want 1",
			capped.Layers[0].SpatialLayerCount)
	}
}

func webRTCSVCBaseStartHasSSForTest(
	t *testing.T,
	payloads []govpx.RTPPayloadFragment,
) bool {
	t.Helper()
	for i, payload := range payloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if desc.SpatialID == 0 && desc.StartOfFrame {
			return desc.ScalabilityStructurePresent
		}
	}
	return false
}

func TestCappedTelemetryReportsTransmittedLayers(t *testing.T) {
	result := encodeOneSVCResultForTest(t)
	capped := limitSVCResultForRTPForTest(t, result, 2)

	tracker := newStatsTracker()
	tracker.observe(capped, time.Now())
	raw, err := tracker.snapshot(capped, defaultBitrateKbps, 0, 2,
		spatialLayerCount, 0, telemetrySender{
			EncodeMs:           1.25,
			PacketizeMs:        0.5,
			WriteMs:            0.25,
			AccessUnitMs:       2,
			ScheduleLagMs:      3.5,
			RTPPackets:         3,
			ForcedKey:          true,
			PacketizerRecovery: true,
			FailedEncodedAUs:   1,
			Withheld:           true,
			WithheldAUs:        2,
			PartialWriteAUs:    3,
			SpatialCapMax:      2,
			CapOverrunStreak:   1,
			CapRecoveryStreak:  17,
		})
	if err != nil {
		t.Fatalf("snapshot capped telemetry: %v", err)
	}
	var msg telemetryMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("decode capped telemetry: %v\npayload=%s", err, raw)
	}
	if len(msg.Layers) != 2 {
		t.Fatalf("capped telemetry layer count = %d, want 2", len(msg.Layers))
	}
	if msg.Totals.Bytes != capped.SizeBytes {
		t.Fatalf("capped telemetry bytes = %d, want %d",
			msg.Totals.Bytes, capped.SizeBytes)
	}
	if msg.Settings.ActiveSpatialLayers != 2 ||
		msg.Settings.RequestedSpatialLayers != spatialLayerCount {
		t.Fatalf("capped telemetry spatial settings = active:%d requested:%d, want 2/%d",
			msg.Settings.ActiveSpatialLayers,
			msg.Settings.RequestedSpatialLayers, spatialLayerCount)
	}
	if msg.Layers[1].SP != 1 {
		t.Fatalf("top transmitted telemetry SP = %d, want 1", msg.Layers[1].SP)
	}
	for i, layer := range msg.Layers {
		wantThreads := pickThreads(layerDims[i][0], layerDims[i][1])
		if layer.Threads != wantThreads {
			t.Fatalf("layer %d telemetry threads = %d, want %d",
				i, layer.Threads, wantThreads)
		}
		if layer.RowMT {
			t.Fatalf("layer %d telemetry RowMT = true for %d tile threads, want false",
				i, wantThreads)
		}
		if layer.TileCols < 1 || layer.TileCols > wantThreads {
			t.Fatalf("layer %d telemetry tile cols = %d, want in [1,%d]",
				i, layer.TileCols, wantThreads)
		}
	}
	if msg.Sender.EncodeMs != 1.25 ||
		msg.Sender.PacketizeMs != 0.5 ||
		msg.Sender.WriteMs != 0.25 ||
		msg.Sender.AccessUnitMs != 2 ||
		msg.Sender.ScheduleLagMs != 3.5 ||
		msg.Sender.RTPPackets != 3 ||
		!msg.Sender.ForcedKey ||
		!msg.Sender.PacketizerRecovery ||
		msg.Sender.FailedEncodedAUs != 1 ||
		!msg.Sender.Withheld ||
		msg.Sender.WithheldAUs != 2 ||
		msg.Sender.PartialWriteAUs != 3 ||
		msg.Sender.SpatialCapMax != 2 ||
		msg.Sender.CapOverrunStreak != 1 ||
		msg.Sender.CapRecoveryStreak != 17 {
		t.Fatalf("sender telemetry = %+v, want timing/recovery counters",
			msg.Sender)
	}
}

func TestTelemetryReportsThreadedTopLayerConfig(t *testing.T) {
	result := encodeOneSVCResultForTest(t)
	tracker := newStatsTracker()
	tracker.observe(result, time.Now())
	raw, err := tracker.snapshot(result, defaultBitrateKbps, 0,
		spatialLayerCount, spatialLayerCount, 0, telemetrySender{})
	if err != nil {
		t.Fatalf("snapshot telemetry: %v", err)
	}
	var msg telemetryMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("decode telemetry: %v\npayload=%s", err, raw)
	}
	if len(msg.Layers) != spatialLayerCount {
		t.Fatalf("telemetry layer count = %d, want %d",
			len(msg.Layers), spatialLayerCount)
	}

	topIndex := spatialLayerCount - 1
	top := msg.Layers[topIndex]
	topWidth, topHeight := layerDims[topIndex][0], layerDims[topIndex][1]
	wantThreads := pickThreads(topWidth, topHeight)
	if top.Threads != wantThreads {
		t.Fatalf("top telemetry threads = %d, want %d",
			top.Threads, wantThreads)
	}
	if top.RowMT {
		t.Fatalf("top telemetry RowMT = true for %d tile threads, want false",
			wantThreads)
	}
	wantTileCols := 1 << uint(expectedTileLog2Cols(wantThreads))
	if top.TileCols != wantTileCols {
		t.Fatalf("top telemetry tile cols = %d, want %d",
			top.TileCols, wantTileCols)
	}
}

func TestStatsTrackerDoesNotCountUnsentEncodedAccessUnit(t *testing.T) {
	result := encodeOneSVCResultForTest(t)
	tracker := newStatsTracker()

	raw, err := tracker.snapshot(result, defaultBitrateKbps, 0,
		spatialLayerCount, spatialLayerCount, 0, telemetrySender{
			Withheld:    true,
			WithheldAUs: 1,
		})
	if err != nil {
		t.Fatalf("snapshot unsent telemetry: %v", err)
	}
	var msg telemetryMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("decode unsent telemetry: %v\npayload=%s", err, raw)
	}
	if msg.Totals.Bytes != result.SizeBytes {
		t.Fatalf("snapshot bytes = %d, want current AU bytes %d",
			msg.Totals.Bytes, result.SizeBytes)
	}
	if msg.Frame != 0 || msg.Totals.FPS != 0 || msg.Totals.KbpsR != 0 {
		t.Fatalf("unsent telemetry counted sent stats: frame=%d fps=%.2f kbps=%.2f",
			msg.Frame, msg.Totals.FPS, msg.Totals.KbpsR)
	}
	if !msg.Sender.Withheld || msg.Sender.WithheldAUs != 1 {
		t.Fatalf("withheld AU telemetry = withheld:%t count:%d, want true/1",
			msg.Sender.Withheld, msg.Sender.WithheldAUs)
	}
	if msg.Sender.FailedEncodedAUs != 0 {
		t.Fatalf("withheld AU counted as failed encoded AU: %d",
			msg.Sender.FailedEncodedAUs)
	}

	tracker.observe(result, time.Now())
	raw, err = tracker.snapshot(result, defaultBitrateKbps, 0,
		spatialLayerCount, spatialLayerCount, 0, telemetrySender{
			RTPPackets: 1,
		})
	if err != nil {
		t.Fatalf("snapshot sent telemetry: %v", err)
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("decode sent telemetry: %v\npayload=%s", err, raw)
	}
	if msg.Frame != 1 {
		t.Fatalf("sent telemetry frame = %d, want 1", msg.Frame)
	}
	if msg.Sender.RTPPackets != 1 {
		t.Fatalf("sent telemetry RTP packets = %d, want 1",
			msg.Sender.RTPPackets)
	}
}

func TestStatsTrackerClearsHiddenLayerWindows(t *testing.T) {
	result := encodeOneSVCResultForTest(t)
	capped := limitSVCResultForRTPForTest(t, result, 1)
	tracker := newStatsTracker()
	start := time.Now()

	tracker.observe(result, start.Add(time.Second))
	if tracker.windowed[1].lastKBPS == 0 || tracker.windowed[2].lastKBPS == 0 {
		t.Fatalf("full-layer warmup kbps = %.2f/%.2f, want non-zero",
			tracker.windowed[1].lastKBPS, tracker.windowed[2].lastKBPS)
	}

	tracker.observe(capped, start.Add(1500*time.Millisecond))
	if tracker.windowed[1].lastKBPS != 0 || tracker.windowed[2].lastKBPS != 0 {
		t.Fatalf("hidden-layer kbps after cap = %.2f/%.2f, want zero",
			tracker.windowed[1].lastKBPS, tracker.windowed[2].lastKBPS)
	}

	tracker.observe(result, start.Add(1600*time.Millisecond))
	raw, err := tracker.snapshot(result, defaultBitrateKbps, 0,
		spatialLayerCount, spatialLayerCount, 0, telemetrySender{})
	if err != nil {
		t.Fatalf("snapshot restored telemetry: %v", err)
	}
	var msg telemetryMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("decode restored telemetry: %v\npayload=%s", err, raw)
	}
	if len(msg.Layers) != spatialLayerCount {
		t.Fatalf("restored telemetry layer count = %d, want %d",
			len(msg.Layers), spatialLayerCount)
	}
	if msg.Settings.ActiveSpatialLayers != spatialLayerCount ||
		msg.Settings.RequestedSpatialLayers != spatialLayerCount {
		t.Fatalf("restored telemetry spatial settings = active:%d requested:%d, want %d/%d",
			msg.Settings.ActiveSpatialLayers,
			msg.Settings.RequestedSpatialLayers, spatialLayerCount,
			spatialLayerCount)
	}
	if msg.Layers[1].KbpsR != 0 || msg.Layers[2].KbpsR != 0 {
		t.Fatalf("restored hidden-layer stale kbps = %.2f/%.2f, want zero until fresh window",
			msg.Layers[1].KbpsR, msg.Layers[2].KbpsR)
	}
}

func limitSVCResultForRTPForTest(
	t *testing.T,
	result govpx.VP9SpatialSVCEncodeResult,
	layerCount int,
) govpx.VP9SpatialSVCEncodeResult {
	t.Helper()
	limited, err := result.LimitSpatialLayersForRTP(layerCount)
	if err != nil {
		t.Fatalf("LimitSpatialLayersForRTP(%d): %v", layerCount, err)
	}
	return limited
}

func webRTCSVCShouldSignalScalabilityStructureForTest(
	layer govpx.VP9EncodeResult,
	result govpx.VP9SpatialSVCEncodeResult,
) bool {
	if !layer.KeyFrame || layer.InterPicturePredicted ||
		layer.TemporalLayerID != 0 {
		return false
	}
	ss := result.ScalabilityStructure
	if ss.SpatialLayerCount != 0 || ss.ResolutionPresent ||
		ss.PictureGroupPresent || len(ss.PictureGroups) != 0 {
		return true
	}
	for i := range ss.Width {
		if ss.Width[i] != 0 || ss.Height[i] != 0 {
			return true
		}
	}
	return false
}

func encodeOneSVCResultForTest(t *testing.T) govpx.VP9SpatialSVCEncodeResult {
	t.Helper()
	return encodeSVCResultsForTest(t, 1)[0]
}

func encodeSVCResultsForTest(t *testing.T, frames int) []govpx.VP9SpatialSVCEncodeResult {
	t.Helper()
	svc, err := newSVCEncoder(demoConfig{
		FPS:         defaultFPS,
		BitrateKbps: defaultBitrateKbps,
	})
	if err != nil {
		t.Fatalf("newSVCEncoder: %v", err)
	}
	defer svc.Close()

	imgs := make([]*image.YCbCr, spatialLayerCount)
	for i := range imgs {
		imgs[i] = image.NewYCbCr(image.Rect(0, 0, layerDims[i][0], layerDims[i][1]),
			image.YCbCrSubsampleRatio420)
	}
	dst := make([]byte, superframeBudget())
	results := make([]govpx.VP9SpatialSVCEncodeResult, frames)
	for frame := 0; frame < frames; frame++ {
		drawScene(imgs, frame)
		result, err := svc.EncodeIntoWithResult(imgs, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		results[frame] = result
	}
	return results
}

func packetizeWebRTCSVCResultForTest(t *testing.T, result govpx.VP9SpatialSVCEncodeResult,
	pictureID uint16, mtu int,
) []govpx.RTPPayloadFragment {
	t.Helper()
	packets, payloadBytes, err := result.WebRTCRTPPacketizationSize(pictureID, mtu)
	if err != nil {
		t.Fatalf("WebRTCRTPPacketizationSize: %v", err)
	}
	payloads := make([]govpx.RTPPayloadFragment, packets)
	payloadBuf := make([]byte, payloadBytes)
	n, used, err := result.PacketizeWebRTCRTPInto(payloads, payloadBuf,
		pictureID, mtu)
	if err != nil {
		t.Fatalf("PacketizeWebRTCRTPInto: %v", err)
	}
	if n != packets || used != payloadBytes {
		t.Fatalf("PacketizeWebRTCRTPInto returned %d/%d, want %d/%d",
			n, used, packets, payloadBytes)
	}
	return payloads[:n]
}

func reassembleWebRTCSVCResultForTest(t *testing.T,
	result govpx.VP9SpatialSVCEncodeResult,
	payloads []govpx.RTPPayloadFragment,
	pictureID uint16,
) []byte {
	t.Helper()
	count := int(result.LayerCount)
	var byLayer [govpx.VP9MaxSpatialLayers][]govpx.RTPPayloadFragment
	for i, payload := range payloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if !desc.PictureIDPresent || !desc.PictureID15Bit ||
			desc.PictureID != pictureID {
			t.Fatalf("payload %d PictureID = present:%t 15bit:%t id:%d, want %d",
				i, desc.PictureIDPresent, desc.PictureID15Bit,
				desc.PictureID, pictureID)
		}
		if got, want := payload.Marker, i == len(payloads)-1; got != want {
			t.Fatalf("payload %d marker = %t, want %t", i, got, want)
		}
		if !desc.LayerIndicesPresent || int(desc.SpatialID) >= count {
			t.Fatalf("payload %d descriptor = %+v, want spatial layer < %d",
				i, desc, count)
		}
		layerID := int(desc.SpatialID)
		wantLayer := result.Layers[layerID]
		if int(desc.TemporalID) != wantLayer.TemporalLayerID ||
			desc.TL0PICIDX != wantLayer.TL0PICIDX ||
			desc.SwitchingUpPoint != wantLayer.TemporalLayerSync ||
			desc.InterPicturePredicted != wantLayer.InterPicturePredicted ||
			desc.InterLayerDependency != wantLayer.InterLayerDependency ||
			desc.NotRefForUpperSpatialLayer != wantLayer.NotRefForUpperSpatialLayer {
			t.Fatalf("payload %d layer %d descriptor = tid:%d tl0:%d sync:%t p:%t dep:%t n:%t, want tid:%d tl0:%d sync:%t p:%t dep:%t n:%t",
				i, layerID, desc.TemporalID, desc.TL0PICIDX,
				desc.SwitchingUpPoint, desc.InterPicturePredicted,
				desc.InterLayerDependency,
				desc.NotRefForUpperSpatialLayer,
				wantLayer.TemporalLayerID, wantLayer.TL0PICIDX,
				wantLayer.TemporalLayerSync, wantLayer.InterPicturePredicted,
				wantLayer.InterLayerDependency,
				wantLayer.NotRefForUpperSpatialLayer)
		}
		if layerID == 0 && desc.StartOfFrame {
			if webRTCSVCShouldSignalScalabilityStructureForTest(wantLayer, result) {
				wantSpatialLayers := count
				if result.ScalabilityStructure.SpatialLayerCount != 0 {
					wantSpatialLayers = result.ScalabilityStructure.SpatialLayerCount
				}
				if !desc.ScalabilityStructurePresent ||
					desc.ScalabilityStructure.SpatialLayerCount != wantSpatialLayers {
					t.Fatalf("base payload %d SS = present:%t layers:%d, want %d",
						i, desc.ScalabilityStructurePresent,
						desc.ScalabilityStructure.SpatialLayerCount,
						wantSpatialLayers)
				}
			} else if desc.ScalabilityStructurePresent {
				t.Fatalf("base delta payload %d repeated scalability structure",
					i)
			}
		} else if desc.ScalabilityStructurePresent {
			t.Fatalf("payload %d layer %d repeated scalability structure",
				i, layerID)
		}
		byLayer[layerID] = append(byLayer[layerID], payload)
	}

	var frames [govpx.VP9MaxSpatialLayers][]byte
	for layerID := 0; layerID < count; layerID++ {
		assembled, err := govpx.AssembleVP9RTPFrame(byLayer[layerID])
		if err != nil {
			t.Fatalf("AssembleVP9RTPFrame layer %d: %v", layerID, err)
		}
		if !bytes.Equal(assembled, result.Layers[layerID].Data) {
			t.Fatalf("assembled RTP layer %d does not match encoded layer",
				layerID)
		}
		frames[layerID] = assembled
	}
	need, err := govpx.VP9SuperframeSize(frames[:count]...)
	if err != nil {
		t.Fatalf("VP9SuperframeSize: %v", err)
	}
	packet := make([]byte, need)
	n, err := govpx.PackVP9SuperframeInto(packet, frames[:count]...)
	if err != nil {
		t.Fatalf("PackVP9SuperframeInto: %v", err)
	}
	return packet[:n]
}

type recordingRTPWriterForTest struct {
	packets []rtp.Packet
	failAt  int
	err     error
}

func (w *recordingRTPWriterForTest) WriteRTP(packet *rtp.Packet) error {
	if w.err != nil && len(w.packets) == w.failAt {
		return w.err
	}
	copyPacket := *packet
	copyPacket.Payload = append([]byte(nil), packet.Payload...)
	w.packets = append(w.packets, copyPacket)
	return nil
}

func assertWebRTCSVCDecoderOutputForTest(
	t *testing.T,
	decoder *govpx.VP9Decoder,
	packet []byte,
	frame int,
	layer int,
	wantWidth int,
	wantHeight int,
) {
	t.Helper()
	if err := decoder.Decode(packet); err != nil {
		t.Fatalf("Decode frame %d layer %d: %v", frame, layer, err)
	}
	img, ok := decoder.NextFrame()
	if !ok {
		t.Fatalf("Decode frame %d layer %d produced no visible frame",
			frame, layer)
	}
	if img.Width != wantWidth || img.Height != wantHeight {
		t.Fatalf("Decode frame %d layer %d image = %dx%d, want %dx%d",
			frame, layer, img.Width, img.Height, wantWidth, wantHeight)
	}
	info, ok := decoder.LastFrameInfo()
	if !ok || !info.ShowFrame || info.Corrupted ||
		info.Width != wantWidth || info.Height != wantHeight {
		t.Fatalf("Decode frame %d layer %d info = %+v ok=%t, want clean %dx%d",
			frame, layer, info, ok, wantWidth, wantHeight)
	}
}

func marshalRTCPForTest(t *testing.T, packet rtcp.Packet) []byte {
	t.Helper()
	raw, err := packet.Marshal()
	if err != nil {
		t.Fatalf("marshal %T: %v", packet, err)
	}
	return raw
}

func expectedThreads(width, height int) int {
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

func expectedTileLog2Cols(threads int) int {
	if threads <= 1 {
		return 0
	}
	if threads <= 2 {
		return 1
	}
	return 2
}
