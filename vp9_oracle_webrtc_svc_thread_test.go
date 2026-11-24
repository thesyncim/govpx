//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"image"
	"runtime"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9WebRTCAutoThreadedSVCStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const frames = 6
	opts, widths, heights := vp9RealtimeWebRTCSVCAutoThreadOptionsForTest()
	layerCount := int(opts.LayerCount)
	topLayer := layerCount - 1
	wantThreads := vp9RealtimeAutoThreadHint(opts.Layers[topLayer],
		runtime.NumCPU())
	if wantThreads <= 1 {
		t.Skip("runtime exposes only one usable VP9 realtime tile thread")
	}
	svc, err := NewVP9SpatialSVCEncoder(opts)
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	defer svc.Close()

	srcs := make([]*image.YCbCr, layerCount)
	dst := make([]byte, 1<<22)
	packetizer := NewVP9WebRTCPacketizer(0x310)
	payloads := make([]RTPPayloadFragment, 0, 64)
	payloadBuf := make([]byte, 0, 1<<20)
	packets := make([][]byte, 0, frames)
	for frame := 0; frame < frames; frame++ {
		for layer := 0; layer < layerCount; layer++ {
			srcs[layer] = vp9test.NewPanningYCbCr(widths[layer],
				heights[layer], frame+layer*3)
		}
		result, err := svc.EncodeIntoWithResult(srcs, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", frame, err)
		}
		if result.LayerCount != opts.LayerCount {
			t.Fatalf("frame %d layer count = %d, want %d",
				frame, result.LayerCount, opts.LayerCount)
		}
		if frame == 0 {
			assertVP9WebRTCAutoThreadedSVCTopLayer(t, svc,
				result, topLayer, wantThreads)
		}

		payloadCount, payloadBytes, err := packetizer.
			SpatialSVCWebRTCNonFlexiblePacketizationSize(result, 500)
		if err != nil {
			t.Fatalf("SpatialSVCWebRTCNonFlexiblePacketizationSize[%d]: %v",
				frame, err)
		}
		if cap(payloads) < payloadCount {
			payloads = make([]RTPPayloadFragment, payloadCount)
		}
		payloads = payloads[:payloadCount]
		if cap(payloadBuf) < payloadBytes {
			payloadBuf = make([]byte, payloadBytes)
		}
		payloadBuf = payloadBuf[:payloadBytes]
		n, used, err := packetizer.
			PacketizeSpatialSVCWebRTCNonFlexibleInto(result, payloads,
				payloadBuf, 500)
		if err != nil {
			t.Fatalf("PacketizeSpatialSVCWebRTCNonFlexibleInto[%d]: %v",
				frame, err)
		}
		if n != payloadCount || used != payloadBytes {
			t.Fatalf("PacketizeSpatialSVCWebRTCNonFlexibleInto[%d] returned %d/%d, want %d/%d",
				frame, n, used, payloadCount, payloadBytes)
		}
		packets = append(packets,
			vp9AssembleWebRTCSVCOracleAccessUnit(t, frame, result,
				payloads[:n]))
	}

	assertVP9SpatialSVCOracleVpxdecLayerOutputs(t,
		"auto-threaded WebRTC SVC", packets, layerCount, widths, heights)
}

func assertVP9WebRTCAutoThreadedSVCTopLayer(t *testing.T,
	svc *VP9SpatialSVCEncoder,
	result VP9SpatialSVCEncodeResult,
	topLayer int,
	wantThreads int,
) {
	t.Helper()
	top := svc.layers[topLayer]
	header, tileStart := vp9test.ParseHeader(t, result.Layers[topLayer].Data)
	if got := 1 << uint(header.Tile.Log2TileCols); got != wantThreads {
		t.Fatalf("auto-threaded WebRTC SVC top tile columns = %d, want %d",
			got, wantThreads)
	}
	if top.vp9TilePool == nil {
		t.Fatal("auto-threaded WebRTC SVC top layer did not initialize tile worker pool")
	}
	if got := top.vp9TilePool.workerCount; got != wantThreads {
		t.Fatalf("auto-threaded WebRTC SVC top worker count = %d, want %d",
			got, wantThreads)
	}
	assertVP9EncoderTilePrefixForTest(t, result.Layers[topLayer].Data,
		tileStart)
}

func vp9AssembleWebRTCSVCOracleAccessUnit(t *testing.T,
	frame int,
	result VP9SpatialSVCEncodeResult,
	payloads []RTPPayloadFragment,
) []byte {
	t.Helper()
	layerCount := int(result.LayerCount)
	var byLayer [VP9MaxSpatialLayers][]RTPPayloadFragment
	for i, payload := range payloads {
		desc, _, err := ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("frame %d ParseVP9RTPPayloadDescriptor[%d]: %v",
				frame, i, err)
		}
		if desc.FlexibleMode {
			t.Fatalf("frame %d payload %d used flexible descriptor", frame, i)
		}
		layerID := int(desc.SpatialID)
		if layerCount > 1 {
			if !desc.LayerIndicesPresent || layerID >= layerCount {
				t.Fatalf("frame %d payload %d descriptor = %+v, want spatial id < %d",
					frame, i, desc, layerCount)
			}
		} else {
			layerID = 0
		}
		if got, want := payload.Marker, i == len(payloads)-1; got != want {
			t.Fatalf("frame %d payload %d marker = %t, want %t",
				frame, i, got, want)
		}
		byLayer[layerID] = append(byLayer[layerID], payload)
	}

	var frames [VP9MaxSpatialLayers][]byte
	for layer := 0; layer < layerCount; layer++ {
		assembled, err := AssembleVP9RTPFrame(byLayer[layer])
		if err != nil {
			t.Fatalf("frame %d AssembleVP9RTPFrame layer %d: %v",
				frame, layer, err)
		}
		if !bytes.Equal(assembled, result.Layers[layer].Data) {
			t.Fatalf("frame %d layer %d RTP reassembly changed payload",
				frame, layer)
		}
		frames[layer] = assembled
	}
	need, err := VP9SuperframeSize(frames[:layerCount]...)
	if err != nil {
		t.Fatalf("frame %d VP9SuperframeSize: %v", frame, err)
	}
	packet := make([]byte, need)
	n, err := PackVP9SuperframeInto(packet, frames[:layerCount]...)
	if err != nil {
		t.Fatalf("frame %d PackVP9SuperframeInto: %v", frame, err)
	}
	if !bytes.Equal(packet[:n], result.Data) {
		t.Fatalf("frame %d WebRTC-reassembled SVC access unit changed payload",
			frame)
	}
	return packet[:n]
}
