//go:build govpx_oracle_trace

package main

import (
	"image"
	"testing"

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

func encodeWebRTCPacketizedAccessUnitsForOracle(t *testing.T, caps []int, forceKeyFrames ...int) [][]byte {
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
	packets := make([][]byte, len(caps))
	pictureID := uint16(0x100)
	lastCap := caps[0]
	forceKeyFrame := make(map[int]bool, len(forceKeyFrames))
	for _, frame := range forceKeyFrames {
		forceKeyFrame[frame] = true
	}
	for frame, cap := range caps {
		if frame > 0 && (cap != lastCap || forceKeyFrame[frame]) {
			forceKeyAll(svc)
		}
		drawScene(imgs, frame)
		result, err := svc.EncodeIntoWithResult(imgs, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
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
