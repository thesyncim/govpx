//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"runtime"
	"testing"
)

func TestVP9WebRTCPreEncodeDropPacketizedStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	for _, tc := range []struct {
		name            string
		dropFrame       int
		droppedTLID     int
		wantRecoveryKey bool
	}{
		{name: "drop-tl2", dropFrame: 1, droppedTLID: 2},
		{name: "drop-tl1", dropFrame: 2, droppedTLID: 1, wantRecoveryKey: true},
		{name: "drop-tl0", dropFrame: 4, droppedTLID: 0, wantRecoveryKey: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const width, height = 64, 64
			e, err := NewVP9Encoder(VP9EncoderOptions{
				Width:              width,
				Height:             height,
				FPS:                30,
				Deadline:           DeadlineRealtime,
				CpuUsed:            8,
				RateControlModeSet: true,
				RateControlMode:    RateControlCBR,
				TargetBitrateKbps:  120,
				DropFrameAllowed:   true,
				TemporalScalability: TemporalScalabilityConfig{
					Enabled: true,
					Mode:    TemporalLayeringThreeLayers,
				},
				ErrorResilient:           true,
				FrameParallelDecodingSet: true,
				FrameParallelDecoding:    true,
			})
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			defer e.Close()

			dst := make([]byte, 1<<20)
			packetizer := NewVP9WebRTCPacketizer(VP9RTPPictureID15BitMask - 2)
			fragments := make([]RTPPayloadFragment, 0, 16)
			payloadBuf := make([]byte, 0, 4096)
			packets := make([][]byte, 0, 10)
			droppedFrames := 0
			for frame := 0; frame < 10; frame++ {
				if frame == tc.dropFrame {
					e.rc.bufferLevelBits = -e.rc.bitsPerFrame - 1
				}
				result, err := e.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(
					width, height, byte(32+frame*11), byte(224-frame*7),
					byte(96+frame*3), byte(192-frame*5)), dst)
				if err != nil {
					t.Fatalf("EncodeIntoWithResult[%d]: %v", frame, err)
				}
				packetsNeeded, payloadBytes, sent, err := packetizer.PacketizationSize(
					result, 89)
				if err != nil {
					t.Fatalf("PacketizationSize[%d]: %v", frame, err)
				}
				if result.Dropped {
					droppedFrames++
					if result.TemporalLayerID != tc.droppedTLID {
						t.Fatalf("dropped frame %d temporal layer = %d, want %d",
							frame, result.TemporalLayerID, tc.droppedTLID)
					}
					if got := packetizer.NeedsKeyFrame(); got != tc.wantRecoveryKey {
						t.Fatalf("NeedsKeyFrame after dropped frame %d = %t, want %t",
							frame, got, tc.wantRecoveryKey)
					}
					if sent || packetsNeeded != 0 || payloadBytes != 0 {
						t.Fatalf("dropped frame %d size = packets:%d bytes:%d sent:%t",
							frame, packetsNeeded, payloadBytes, sent)
					}
					if _, _, sent, err := packetizer.PacketizeInto(result, nil, nil,
						89); err != nil || sent {
						t.Fatalf("dropped PacketizeInto[%d] = sent:%t err:%v",
							frame, sent, err)
					}
					if err := e.SetFrameDropAllowed(false); err != nil {
						t.Fatalf("SetFrameDropAllowed(false): %v", err)
					}
					if packetizer.NeedsKeyFrame() {
						e.ForceKeyFrame()
					}
					continue
				}
				if !sent {
					t.Fatalf("non-dropped frame %d reported unsent size", frame)
				}
				if cap(fragments) < packetsNeeded {
					fragments = make([]RTPPayloadFragment, packetsNeeded)
				}
				fragments = fragments[:packetsNeeded]
				if cap(payloadBuf) < payloadBytes {
					payloadBuf = make([]byte, payloadBytes)
				}
				payloadBuf = payloadBuf[:payloadBytes]
				n, used, sent, err := packetizer.PacketizeInto(result, fragments,
					payloadBuf, 89)
				if err != nil || !sent {
					t.Fatalf("PacketizeInto[%d] = packets:%d bytes:%d sent:%t err:%v",
						frame, n, used, sent, err)
				}
				if n != packetsNeeded || used != payloadBytes {
					t.Fatalf("PacketizeInto[%d] returned %d/%d, want %d/%d",
						frame, n, used, packetsNeeded, payloadBytes)
				}
				assembled, err := AssembleVP9RTPFrame(fragments[:n])
				if err != nil {
					t.Fatalf("AssembleVP9RTPFrame[%d]: %v", frame, err)
				}
				if !bytes.Equal(assembled, result.Data) {
					t.Fatalf("frame %d WebRTC RTP reassembly drifted", frame)
				}
				packets = append(packets, append([]byte(nil), assembled...))
			}
			if droppedFrames != 1 {
				t.Fatalf("pre-encode dropped frames = %d, want 1", droppedFrames)
			}

			ivf := vp9test.BuildVP9IVF(width, height, packets...)
			raw := vp9test.VpxdecI420(t, ivf)
			want := len(packets) * width * height * 3 / 2
			if len(raw) != want {
				t.Fatalf("vpxdec raw size = %d, want %d", len(raw), want)
			}
		})
	}
}

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
