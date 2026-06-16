//go:build govpx_oracle_trace

package main

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestWebRTCPacketizedSVCStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	packets := encodeWebRTCPacketizedAccessUnitsForOracle(t,
		[]int{3, 3, 3, 3})
	ivf := vp9test.BuildVP9IVF(layerDims[spatialLayerCount-1][0],
		layerDims[spatialLayerCount-1][1], packets...)
	for layer := 0; layer < spatialLayerCount; layer++ {
		raw := vp9test.VpxdecI420WithOptions(t, ivf, vp9test.VpxdecOptions{
			SVCSpatialLayerSet: true,
			SVCSpatialLayer:    layer,
		})
		want := len(packets) * layerDims[layer][0] * layerDims[layer][1] * 3 / 2
		if len(raw) != want {
			t.Fatalf("vpxdec layer %d raw size = %d, want %d",
				layer, len(raw), want)
		}
	}
}

func TestWebRTCPacketizedSVCForcedKeyStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	packets := encodeWebRTCPacketizedAccessUnitsForOracle(t,
		[]int{3, 3, 3, 3, 3}, 2)
	ivf := vp9test.BuildVP9IVF(layerDims[spatialLayerCount-1][0],
		layerDims[spatialLayerCount-1][1], packets...)
	for layer := 0; layer < spatialLayerCount; layer++ {
		raw := vp9test.VpxdecI420WithOptions(t, ivf, vp9test.VpxdecOptions{
			SVCSpatialLayerSet: true,
			SVCSpatialLayer:    layer,
		})
		want := len(packets) * layerDims[layer][0] * layerDims[layer][1] * 3 / 2
		if len(raw) != want {
			t.Fatalf("forced-key vpxdec layer %d raw size = %d, want %d",
				layer, len(raw), want)
		}
	}
}

func TestWebRTCPacketizedSVCPeriodicKeyStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const keyInterval = 4
	caps := []int{3, 3, 3, 3, 3, 3, 3, 3, 3}
	packets := encodeWebRTCPacketizedPeriodicKeyAccessUnitsForOracle(t,
		caps, keyInterval)
	ivf := vp9test.BuildVP9IVF(layerDims[spatialLayerCount-1][0],
		layerDims[spatialLayerCount-1][1], packets...)

	for layer := 0; layer < spatialLayerCount; layer++ {
		raw := vp9test.VpxdecI420WithOptions(t, ivf, vp9test.VpxdecOptions{
			SVCSpatialLayerSet: true,
			SVCSpatialLayer:    layer,
		})
		want := capRecoveryVpxdecBytesForLayer(caps, layer)
		if len(raw) != want {
			t.Fatalf("periodic-key vpxdec layer %d raw size = %d, want %d",
				layer, len(raw), want)
		}
	}
}

func TestWebRTCPacketizedSVCCapRecoveryStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	caps := []int{3, 3, 2, 2, 1, 3, 3, 2, 3}
	packets := encodeWebRTCPacketizedAccessUnitsForOracle(t, caps)
	ivf := vp9test.BuildVP9IVF(layerDims[spatialLayerCount-1][0],
		layerDims[spatialLayerCount-1][1], packets...)

	for layer := 0; layer < spatialLayerCount; layer++ {
		raw := vp9test.VpxdecI420WithOptions(t, ivf, vp9test.VpxdecOptions{
			SVCSpatialLayerSet: true,
			SVCSpatialLayer:    layer,
		})
		want := capRecoveryVpxdecBytesForLayer(caps, layer)
		if len(raw) != want {
			t.Fatalf("cap-recovery vpxdec layer %d raw size = %d, want %d",
				layer, len(raw), want)
		}
	}
}

func TestWebRTCPacketizedSVCRuntimeControlStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	steps := []webRTCSVCOracleStep{
		{cap: 3},
		{cap: 3, bitrateKbps: 1200},
		{cap: 3, screenMode: 1, screenModeSet: true},
		{cap: 2},
		{cap: 2, bitrateKbps: 500},
		{cap: 3, forceKey: true},
		{cap: 3, screenMode: 2, screenModeSet: true},
		{cap: 1},
		{cap: 3, bitrateKbps: 900, screenMode: 0, screenModeSet: true},
	}
	packets := encodeWebRTCPacketizedRuntimeAccessUnitsForOracle(t, steps)
	ivf := vp9test.BuildVP9IVF(layerDims[spatialLayerCount-1][0],
		layerDims[spatialLayerCount-1][1], packets...)

	caps := make([]int, len(steps))
	for i, step := range steps {
		caps[i] = step.cap
	}
	for layer := 0; layer < spatialLayerCount; layer++ {
		raw := vp9test.VpxdecI420WithOptions(t, ivf, vp9test.VpxdecOptions{
			SVCSpatialLayerSet: true,
			SVCSpatialLayer:    layer,
		})
		want := capRecoveryVpxdecBytesForLayer(caps, layer)
		if len(raw) != want {
			t.Fatalf("runtime-control vpxdec layer %d raw size = %d, want %d",
				layer, len(raw), want)
		}
	}
}

func capRecoveryVpxdecBytesForLayer(caps []int, layer int) int {
	total := 0
	for _, cap := range caps {
		if cap <= 0 {
			continue
		}
		outputLayer := layer
		if outputLayer >= cap {
			outputLayer = cap - 1
		}
		total += layerDims[outputLayer][0] * layerDims[outputLayer][1] * 3 / 2
	}
	return total
}

type webRTCSVCOracleStep struct {
	cap           int
	bitrateKbps   int
	screenMode    int
	screenModeSet bool
	forceKey      bool
}

func encodeWebRTCPacketizedAccessUnitsForOracle(t *testing.T, caps []int, forceKeyFrames ...int) [][]byte {
	t.Helper()
	steps := make([]webRTCSVCOracleStep, len(caps))
	for frame, cap := range caps {
		steps[frame] = webRTCSVCOracleStep{cap: cap}
	}
	for _, frame := range forceKeyFrames {
		if frame >= 0 && frame < len(steps) {
			steps[frame].forceKey = true
		}
	}
	return encodeWebRTCPacketizedRuntimeAccessUnitsForOracle(t, steps)
}

func encodeWebRTCPacketizedPeriodicKeyAccessUnitsForOracle(t *testing.T, caps []int, keyInterval int) [][]byte {
	t.Helper()
	steps := make([]webRTCSVCOracleStep, len(caps))
	for frame, cap := range caps {
		steps[frame] = webRTCSVCOracleStep{cap: cap}
	}
	return encodeWebRTCPacketizedRuntimeAccessUnitsForOracleWithHooks(t,
		steps,
		func(svc *govpx.VP9SpatialSVCEncoder) {
			if err := svc.SetLayerKeyFrameInterval(0, keyInterval); err != nil {
				t.Fatalf("SetLayerKeyFrameInterval: %v", err)
			}
		},
		func(frame int, result govpx.VP9SpatialSVCEncodeResult) {
			wantBaseKey := frame == 0 || frame%keyInterval == 0
			if result.Layers[0].KeyFrame != wantBaseKey {
				t.Fatalf("frame %d base key = %t, want %t",
					frame, result.Layers[0].KeyFrame, wantBaseKey)
			}
			if !wantBaseKey {
				return
			}
			for spatial := 1; spatial < int(result.LayerCount); spatial++ {
				layer := result.Layers[spatial]
				if layer.KeyFrame || !layer.ShowFrame {
					t.Fatalf("frame %d layer %d = key:%t show:%t, want visible inter-layer refresh",
						frame, spatial, layer.KeyFrame, layer.ShowFrame)
				}
			}
		})
}

func encodeWebRTCPacketizedRuntimeAccessUnitsForOracle(t *testing.T, steps []webRTCSVCOracleStep) [][]byte {
	t.Helper()
	return encodeWebRTCPacketizedRuntimeAccessUnitsForOracleWithHooks(t,
		steps, nil, nil)
}

func encodeWebRTCPacketizedRuntimeAccessUnitsForOracleWithHooks(
	t *testing.T,
	steps []webRTCSVCOracleStep,
	configure func(*govpx.VP9SpatialSVCEncoder),
	inspect func(int, govpx.VP9SpatialSVCEncodeResult),
) [][]byte {
	t.Helper()
	if len(steps) == 0 {
		return nil
	}
	svc, err := newSVCEncoder(demoConfig{
		FPS:         defaultFPS,
		BitrateKbps: defaultBitrateKbps,
	})
	if err != nil {
		t.Fatalf("newSVCEncoder: %v", err)
	}
	defer svc.Close()
	if configure != nil {
		configure(svc)
	}

	imgs := make([]*image.YCbCr, spatialLayerCount)
	for i := range imgs {
		imgs[i] = image.NewYCbCr(image.Rect(0, 0, layerDims[i][0], layerDims[i][1]),
			image.YCbCrSubsampleRatio420)
	}
	dst := make([]byte, superframeBudget())
	packets := make([][]byte, len(steps))
	pictureID := uint16(0x100)
	lastCap := steps[0].cap
	currentBitrate := defaultBitrateKbps
	currentScreen := 0
	for frame, step := range steps {
		cap := step.cap
		if cap < 1 || cap > spatialLayerCount {
			t.Fatalf("oracle step %d cap = %d, want 1..%d",
				frame, cap, spatialLayerCount)
		}
		if step.bitrateKbps != 0 && step.bitrateKbps != currentBitrate {
			if err := applyBitrate(svc, step.bitrateKbps); err != nil {
				t.Fatalf("applyBitrate frame %d: %v", frame, err)
			}
			currentBitrate = step.bitrateKbps
		}
		if step.screenModeSet && step.screenMode != currentScreen {
			if err := applyScreenMode(svc, step.screenMode); err != nil {
				t.Fatalf("applyScreenMode frame %d: %v", frame, err)
			}
			currentScreen = step.screenMode
		}
		if frame > 0 && (cap != lastCap || step.forceKey) {
			forceKeyAll(svc)
		}
		drawScene(imgs, frame)
		result, err := svc.EncodeIntoWithResult(imgs, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		if inspect != nil {
			inspect(frame, result)
		}
		rtpResult := cappedSVCResultForRTP(result, cap)
		payloads := packetizeWebRTCSVCResultForTest(t, rtpResult, pictureID, 500)
		packets[frame] = append([]byte(nil),
			reassembleWebRTCSVCResultForTest(t, rtpResult, payloads, pictureID)...)
		pictureID = nextVP9PictureID(pictureID)
		lastCap = cap
	}
	return packets
}
