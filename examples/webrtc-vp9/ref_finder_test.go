package main

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx"
)

const vp9WebRTCRefFinderMaxTemporalLayersForTest = 8

func TestWebRTCPacketizedSVCPassesLibwebrtcVP9RefFinder(t *testing.T) {
	steps := []struct {
		cap      int
		forceKey bool
	}{
		{cap: 3, forceKey: true},
		{cap: 3},
		{cap: 3},
		{cap: 3},
		{cap: 1},
		{cap: 1},
		{cap: 1},
		{cap: 3},
		{cap: 3},
		{cap: 2},
		{cap: 2},
		{cap: 3},
		{cap: 3, forceKey: true},
		{cap: 3},
		{cap: 3},
		{cap: 3},
	}
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
	refFinder := newWebRTCVP9RefFinderForTest()
	pictureID := uint16(govpx.VP9RTPPictureID15BitMask - 3)
	lastCap := steps[0].cap
	for frame, step := range steps {
		if frame == 0 || step.forceKey || step.cap != lastCap {
			forceKeyAll(svc)
		}
		drawScene(imgs, frame)
		result, err := svc.EncodeIntoWithResult(imgs, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		rtpResult := limitSVCResultForRTPForTest(t, result, step.cap)
		payloads := packetizeWebRTCSVCResultForTest(t, rtpResult, pictureID, 500)
		refFinder.acceptAccessUnit(t, frame, rtpResult, payloads, pictureID)
		pictureID = govpx.NextVP9RTPPictureID(pictureID)
		lastCap = step.cap
	}
}

func TestWebRTCPacketizedSVCPassesRefFinderAcrossTL0Wrap(t *testing.T) {
	svc, imgs := newSmallWebRTCSVCTestEncoder(t)
	defer svc.Close()

	dst := make([]byte, 1<<20)
	refFinder := newWebRTCVP9RefFinderForTest()
	pictureID := uint16(0x1200)
	var lastTL0 uint8
	var haveTL0 bool
	var sawWrap bool
	for frame := 0; frame < 1032; frame++ {
		drawSmallWebRTCTestFrame(imgs, frame)
		result, err := svc.EncodeIntoWithResult(imgs, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		base := result.Layers[0]
		if base.TemporalLayerID == 0 {
			if haveTL0 && base.TL0PICIDX < lastTL0 {
				sawWrap = true
			}
			lastTL0 = base.TL0PICIDX
			haveTL0 = true
		}
		payloads := packetizeWebRTCSVCResultForTest(t, result, pictureID, 500)
		refFinder.acceptAccessUnit(t, frame, result, payloads, pictureID)
		pictureID = govpx.NextVP9RTPPictureID(pictureID)
	}
	if !sawWrap {
		t.Fatal("test did not cross TL0PICIDX wrap")
	}
}

func newSmallWebRTCSVCTestEncoder(t *testing.T) (
	*govpx.VP9SpatialSVCEncoder,
	[]*image.YCbCr,
) {
	t.Helper()
	dims := [3][2]int{{16, 16}, {32, 32}, {64, 64}}
	bitrates := [3]int{80, 160, 320}
	temporal := govpx.TemporalScalabilityConfig{
		Enabled: true,
		Mode:    govpx.TemporalLayeringThreeLayers,
	}
	var layers [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions
	imgs := make([]*image.YCbCr, len(dims))
	for i := range dims {
		w, h := dims[i][0], dims[i][1]
		layers[i] = govpx.VP9EncoderOptions{
			Width:                    w,
			Height:                   h,
			FPS:                      defaultFPS,
			Deadline:                 govpx.DeadlineRealtime,
			CpuUsed:                  8,
			RateControlModeSet:       true,
			RateControlMode:          govpx.RateControlCBR,
			TargetBitrateKbps:        bitrates[i],
			TemporalScalability:      temporal,
			ErrorResilient:           true,
			FrameParallelDecodingSet: true,
			FrameParallelDecoding:    true,
			MaxKeyframeInterval:      2048,
		}
		imgs[i] = image.NewYCbCr(image.Rect(0, 0, w, h),
			image.YCbCrSubsampleRatio420)
	}
	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount:           uint8(len(dims)),
		InterLayerPrediction: true,
		Layers:               layers,
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	return svc, imgs
}

func drawSmallWebRTCTestFrame(imgs []*image.YCbCr, frame int) {
	for layer, img := range imgs {
		for y := 0; y < img.Rect.Dy(); y++ {
			row := img.Y[y*img.YStride:]
			for x := 0; x < img.Rect.Dx(); x++ {
				row[x] = uint8(32 + (x*3+y*5+frame*7+layer*19)%192)
			}
		}
		for y := 0; y < img.Rect.Dy()/2; y++ {
			cbRow := img.Cb[y*img.CStride:]
			crRow := img.Cr[y*img.CStride:]
			for x := 0; x < img.Rect.Dx()/2; x++ {
				cbRow[x] = uint8(96 + (x*5+frame+layer*11)%64)
				crRow[x] = uint8(128 + (y*7+frame*3+layer*13)%64)
			}
		}
	}
}

type webRTCVP9RefFinderForTest struct {
	gofByTL0           map[int]*webRTCVP9GofInfoForTest
	available          map[int64]bool
	upSwitch           map[uint16]uint8
	missingFramesByTID [vp9WebRTCRefFinderMaxTemporalLayersForTest]map[uint16]bool
	lastUnwrappedTL0   int
	haveUnwrappedTL0   bool
	lastUnwrappedPicID int
	haveUnwrappedPicID bool
}

type webRTCVP9GofInfoForTest struct {
	groups        []govpx.VP9RTPPictureGroup
	pidStart      uint16
	lastPictureID uint16
}

func newWebRTCVP9RefFinderForTest() *webRTCVP9RefFinderForTest {
	f := &webRTCVP9RefFinderForTest{
		gofByTL0:  make(map[int]*webRTCVP9GofInfoForTest),
		available: make(map[int64]bool),
		upSwitch:  make(map[uint16]uint8),
	}
	for i := range f.missingFramesByTID {
		f.missingFramesByTID[i] = make(map[uint16]bool)
	}
	return f
}

func (f *webRTCVP9RefFinderForTest) acceptAccessUnit(
	t *testing.T,
	frame int,
	result govpx.VP9SpatialSVCEncodeResult,
	payloads []govpx.RTPPayloadFragment,
	pictureID uint16,
) {
	t.Helper()
	var starts []govpx.VP9RTPPayloadDescriptor
	for i, payload := range payloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("frame %d ParseVP9RTPPayloadDescriptor[%d]: %v",
				frame, i, err)
		}
		if desc.StartOfFrame {
			starts = append(starts, desc)
		}
	}
	if len(starts) != int(result.LayerCount) {
		t.Fatalf("frame %d layer starts = %d, want %d",
			frame, len(starts), result.LayerCount)
	}
	for layer, desc := range starts {
		if desc.PictureID != pictureID {
			t.Fatalf("frame %d layer %d PictureID = %d, want %d",
				frame, layer, desc.PictureID, pictureID)
		}
		f.acceptFrame(t, frame, layer, desc)
	}
}

func (f *webRTCVP9RefFinderForTest) acceptFrame(
	t *testing.T,
	frame int,
	layer int,
	desc govpx.VP9RTPPayloadDescriptor,
) {
	t.Helper()
	if !desc.LayerIndicesPresent {
		t.Fatalf("frame %d layer %d missing VP9 layer indices", frame, layer)
	}
	if int(desc.TemporalID) >= vp9WebRTCRefFinderMaxTemporalLayersForTest {
		t.Fatalf("frame %d layer %d temporal id = %d",
			frame, layer, desc.TemporalID)
	}
	tl0 := f.unwrapTL0(desc.TL0PICIDX)
	if desc.PictureIDPresent {
		_ = f.unwrapPictureID(desc.PictureID)
	}
	isBaseKey := !desc.InterPicturePredicted && desc.SpatialID == 0 &&
		!desc.InterLayerDependency
	var info *webRTCVP9GofInfoForTest
	if desc.ScalabilityStructurePresent && desc.TemporalID == 0 {
		info = newWebRTCVP9GofInfoForTest(desc.ScalabilityStructure,
			desc.PictureID)
		f.gofByTL0[tl0] = info
		if isBaseKey {
			f.frameReceived(desc.PictureID, info)
			f.markAvailable(desc.PictureID, desc.SpatialID)
			return
		}
	} else {
		if isBaseKey {
			t.Fatalf("frame %d layer %d keyframe reached receiver without SS",
				frame, layer)
		}
		lookupTL0 := tl0
		if desc.TemporalID == 0 && !desc.InterLayerDependency {
			lookupTL0 = tl0 - 1
		}
		info = f.gofByTL0[lookupTL0]
		if info == nil {
			t.Fatalf("frame %d layer %d missing GOF info for TL0 %d",
				frame, layer, lookupTL0)
		}
		if desc.TemporalID == 0 {
			info = &webRTCVP9GofInfoForTest{
				groups:        info.groups,
				pidStart:      desc.PictureID,
				lastPictureID: desc.PictureID,
			}
			f.gofByTL0[tl0] = info
		}
	}
	f.frameReceived(desc.PictureID, info)
	gofIdx := f.gofIndex(desc.PictureID, info)
	if f.missingRequiredFrame(desc.PictureID, info, gofIdx) {
		t.Fatalf("frame %d layer %d would be stashed by libwebrtc VP9 ref finder",
			frame, layer)
	}
	if desc.SwitchingUpPoint {
		f.upSwitch[desc.PictureID] = desc.TemporalID
	}
	if desc.InterPicturePredicted {
		group := info.groups[gofIdx]
		for i := 0; i < group.ReferenceIndexCount; i++ {
			refPictureID := vp9WebRTCPictureIDSub(desc.PictureID,
				group.ReferenceIndices[i])
			if f.upSwitchInInterval(desc.PictureID, desc.TemporalID,
				refPictureID) {
				continue
			}
			f.requireAvailable(t, frame, layer, refPictureID,
				desc.SpatialID, "GOF")
		}
	}
	if desc.InterLayerDependency {
		if desc.SpatialID == 0 {
			t.Fatalf("frame %d layer %d base layer has inter-layer dependency",
				frame, layer)
		}
		f.requireAvailable(t, frame, layer, desc.PictureID,
			desc.SpatialID-1, "inter-layer")
	}
	f.markAvailable(desc.PictureID, desc.SpatialID)
}

func newWebRTCVP9GofInfoForTest(
	ss govpx.VP9RTPScalabilityStructure,
	pictureID uint16,
) *webRTCVP9GofInfoForTest {
	groups := ss.PictureGroups
	if !ss.PictureGroupPresent || len(groups) == 0 {
		groups = []govpx.VP9RTPPictureGroup{{TemporalID: 0}}
	}
	copied := append([]govpx.VP9RTPPictureGroup(nil), groups...)
	return &webRTCVP9GofInfoForTest{
		groups:        copied,
		pidStart:      pictureID,
		lastPictureID: pictureID,
	}
}

func (f *webRTCVP9RefFinderForTest) frameReceived(
	pictureID uint16,
	info *webRTCVP9GofInfoForTest,
) {
	if vp9WebRTCPictureIDAheadOf(pictureID, info.lastPictureID) {
		gofIdx := f.gofIndex(info.lastPictureID, info)
		next := govpx.NextVP9RTPPictureID(info.lastPictureID)
		for next != pictureID {
			gofIdx = (gofIdx + 1) % len(info.groups)
			tid := info.groups[gofIdx].TemporalID
			if int(tid) < len(f.missingFramesByTID) {
				f.missingFramesByTID[tid][next] = true
			}
			next = govpx.NextVP9RTPPictureID(next)
		}
		info.lastPictureID = pictureID
		return
	}
	gofIdx := f.gofIndex(pictureID, info)
	tid := info.groups[gofIdx].TemporalID
	if int(tid) < len(f.missingFramesByTID) {
		delete(f.missingFramesByTID[tid], pictureID)
	}
}

func (f *webRTCVP9RefFinderForTest) missingRequiredFrame(
	pictureID uint16,
	info *webRTCVP9GofInfoForTest,
	gofIdx int,
) bool {
	group := info.groups[gofIdx]
	for i := 0; i < group.ReferenceIndexCount; i++ {
		refPictureID := vp9WebRTCPictureIDSub(pictureID,
			group.ReferenceIndices[i])
		for tid := uint8(0); tid < group.TemporalID; tid++ {
			for missing := range f.missingFramesByTID[tid] {
				if vp9WebRTCPictureIDAheadOf(missing, refPictureID) &&
					vp9WebRTCPictureIDAheadOf(pictureID, missing) {
					return true
				}
			}
		}
	}
	return false
}

func (f *webRTCVP9RefFinderForTest) upSwitchInInterval(
	pictureID uint16,
	temporalID uint8,
	refPictureID uint16,
) bool {
	for upSwitchID, upSwitchTemporalID := range f.upSwitch {
		if vp9WebRTCPictureIDAheadOf(upSwitchID, refPictureID) &&
			vp9WebRTCPictureIDAheadOf(pictureID, upSwitchID) &&
			upSwitchTemporalID < temporalID {
			return true
		}
	}
	return false
}

func (f *webRTCVP9RefFinderForTest) gofIndex(
	pictureID uint16,
	info *webRTCVP9GofInfoForTest,
) int {
	return vp9WebRTCPictureIDForwardDiff(info.pidStart, pictureID) %
		len(info.groups)
}

func (f *webRTCVP9RefFinderForTest) requireAvailable(
	t *testing.T,
	frame int,
	layer int,
	pictureID uint16,
	spatialID uint8,
	reason string,
) {
	t.Helper()
	if !f.available[vp9WebRTCFrameIDForTest(pictureID, spatialID)] {
		t.Fatalf("frame %d layer %d missing %s reference pid=%d sid=%d",
			frame, layer, reason, pictureID, spatialID)
	}
}

func (f *webRTCVP9RefFinderForTest) markAvailable(
	pictureID uint16,
	spatialID uint8,
) {
	f.available[vp9WebRTCFrameIDForTest(pictureID, spatialID)] = true
}

func (f *webRTCVP9RefFinderForTest) unwrapTL0(v uint8) int {
	if !f.haveUnwrappedTL0 {
		f.lastUnwrappedTL0 = int(v)
		f.haveUnwrappedTL0 = true
		return f.lastUnwrappedTL0
	}
	f.lastUnwrappedTL0 = vp9WebRTCUnwrap8ForTest(f.lastUnwrappedTL0, v)
	return f.lastUnwrappedTL0
}

func (f *webRTCVP9RefFinderForTest) unwrapPictureID(v uint16) int {
	if !f.haveUnwrappedPicID {
		f.lastUnwrappedPicID = int(v)
		f.haveUnwrappedPicID = true
		return f.lastUnwrappedPicID
	}
	f.lastUnwrappedPicID = vp9WebRTCUnwrap15ForTest(f.lastUnwrappedPicID, v)
	return f.lastUnwrappedPicID
}

func vp9WebRTCFrameIDForTest(pictureID uint16, spatialID uint8) int64 {
	return int64(pictureID)*govpx.VP9RTPMaxSpatialLayers + int64(spatialID)
}

func vp9WebRTCPictureIDSub(pictureID uint16, diff uint8) uint16 {
	mod := int(govpx.VP9RTPPictureID15BitMask) + 1
	return uint16((int(pictureID) - int(diff) + mod) % mod)
}

func vp9WebRTCPictureIDForwardDiff(from uint16, to uint16) int {
	mod := int(govpx.VP9RTPPictureID15BitMask) + 1
	return (int(to) - int(from) + mod) % mod
}

func vp9WebRTCPictureIDAheadOf(a uint16, b uint16) bool {
	diff := vp9WebRTCPictureIDForwardDiff(b, a)
	return diff > 0 && diff < (int(govpx.VP9RTPPictureID15BitMask)+1)/2
}

func vp9WebRTCUnwrap8ForTest(prev int, value uint8) int {
	return vp9WebRTCUnwrapModuloForTest(prev, int(value), 256)
}

func vp9WebRTCUnwrap15ForTest(prev int, value uint16) int {
	return vp9WebRTCUnwrapModuloForTest(prev, int(value),
		int(govpx.VP9RTPPictureID15BitMask)+1)
}

func vp9WebRTCUnwrapModuloForTest(prev int, value int, mod int) int {
	base := prev - positiveModForTest(prev, mod)
	best := base + value
	for _, candidate := range []int{best - mod, best + mod} {
		if absIntForTest(candidate-prev) < absIntForTest(best-prev) {
			best = candidate
		}
	}
	return best
}

func positiveModForTest(v int, mod int) int {
	r := v % mod
	if r < 0 {
		r += mod
	}
	return r
}

func absIntForTest(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
