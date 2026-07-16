package govpx

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestVP9NonrdPredCtxMatchesLegacyPredictor is the exhaustive byte gate for
// the prepared-context candidate predictor: every block shape x interp filter
// x MV class (zero copy, full-pel, half/quarter/odd subpel, one-axis subpel,
// frame-edge crossing, beyond-clamp extremes) x block position (interior,
// corners, ragged right/bottom edge) must produce byte-identical predictor
// output and identical (variance, sse, ok) versus BOTH the legacy scratch
// path (predictVP9InterBlockLumaToScratch) and the original decoder-recon
// path (predictVP9InterBlockLumaOnly writing the live recon rect).
//
// The registry's rejected "scratch-only filter-search convolve" probe
// mismatched exactly because it skipped ClampMvToUmvBorderSb and window
// normalization; this gate pins the clamp semantics for every cell,
// including MVs that only differ after clamping.
func TestVP9NonrdPredCtxMatchesLegacyPredictor(t *testing.T) {
	for _, dims := range []struct{ w, h int }{
		{128, 128}, // 8-aligned dims: zero-MV copy fast path active
		{94, 78},   // ragged dims: replicated-edge reads on right/bottom
	} {
		t.Run(fmt.Sprintf("%dx%d", dims.w, dims.h), func(t *testing.T) {
			testVP9NonrdPredCtxMatchesLegacy(t, dims.w, dims.h)
		})
	}
}

func testVP9NonrdPredCtxMatchesLegacy(t *testing.T, width, height int) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height,
		CpuUsed: 8})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if !e.refFrames[0].valid {
		t.Fatal("LAST reference was not refreshed by keyframe")
	}
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 3, -2)
	inter := &vp9InterEncodeState{
		img:     interSrc,
		ref:     &e.refFrames[0],
		allowHP: true,
	}
	e.prepareVP9EncoderOutputFrame(width, height)
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3

	shapes := []common.BlockSize{
		common.Block64x64, common.Block64x32, common.Block32x64,
		common.Block32x32, common.Block32x16, common.Block16x32,
		common.Block16x16, common.Block16x8, common.Block8x16,
		common.Block8x8,
	}
	filters := []vp9dec.InterpFilter{
		vp9dec.InterpEighttap, vp9dec.InterpEighttapSmooth,
		vp9dec.InterpEighttapSharp, vp9dec.InterpBilinear,
	}
	mvs := []vp9dec.MV{
		{},                                // zero (copy fast path on aligned dims)
		{Row: -16, Col: 24},               // full-pel interior
		{Row: 4, Col: 4},                  // half-pel both axes
		{Row: 2, Col: 6},                  // quarter/three-quarter
		{Row: 3, Col: -5},                 // odd subpel both axes
		{Row: 7, Col: 0},                  // vertical-only subpel
		{Row: 0, Col: 5},                  // horizontal-only subpel
		{Row: -80, Col: -80},              // crosses top/left frame edge from corner
		{Row: 512, Col: 512},              // deep into bottom/right border
		{Row: -32768 / 2, Col: 32767 / 2}, // beyond-clamp extremes
	}

	for _, bsize := range shapes {
		blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
		blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
		bwMi := int(common.Num8x8BlocksWideLookup[bsize])
		bhMi := int(common.Num8x8BlocksHighLookup[bsize])
		positions := []struct {
			name         string
			miRow, miCol int
		}{
			{"corner", 0, 0},
			{"interior", min(2, max(0, miRows-bhMi)), min(2, max(0, miCols-bwMi))},
			{"bottomright", max(0, miRows-bhMi), max(0, miCols-bwMi)},
			{"rightedge", 0, max(0, miCols-bwMi)},
		}
		for _, pos := range positions {
			if pos.miRow+bhMi > miRows || pos.miCol+bwMi > miCols {
				continue
			}
			var ctx vp9NonrdPredBlockCtx
			if !e.vp9NonrdPredBlockCtxInit(&ctx, inter, pos.miRow, pos.miCol,
				bsize) {
				t.Fatalf("%v %s: block ctx init failed", bsize, pos.name)
			}
			if !e.vp9NonrdPredBlockCtxAddRef(&ctx, vp9dec.LastFrame,
				&e.refFrames[0]) {
				t.Fatalf("%v %s: ref ctx init failed", bsize, pos.name)
			}
			for _, filter := range filters {
				for mvIdx, mv := range mvs {
					name := fmt.Sprintf("%v/%s/f%d/mv%d", bsize, pos.name,
						filter, mvIdx)
					mi := vp9dec.NeighborMi{
						SbType:       bsize,
						Mode:         common.NewMv,
						InterpFilter: uint8(filter),
						RefFrame: [2]int8{vp9dec.LastFrame,
							vp9dec.NoRefFrame},
						Mv: [2]vp9dec.MV{mv},
					}

					// Legacy scratch path.
					want := make([]byte, blockW*blockH)
					for i := range want {
						want[i] = 0x5a
					}
					if !e.predictVP9InterBlockLumaToScratch(inter, pos.miRow,
						pos.miCol, bsize, &mi, want, blockW) {
						t.Fatalf("%s: legacy scratch predictor !ok", name)
					}

					// Original decoder-recon path (the pre-P1.1 substrate).
					if !e.predictVP9InterBlockLumaOnly(inter, miRows, miCols,
						pos.miRow, pos.miCol, bsize, &mi) {
						t.Fatalf("%s: recon predictor !ok", name)
					}
					recon, reconStride := e.vp9EncoderReconPlane(0)
					reconRect := make([]byte, blockW*blockH)
					vp9CopyPredRectToScratch(reconRect, recon, reconStride,
						pos.miCol*common.MiSize, pos.miRow*common.MiSize,
						blockW, blockH)
					if !bytes.Equal(want, reconRect) {
						t.Fatalf("%s: legacy scratch != recon rect", name)
					}

					// Prepared-context path, padded destination stride to
					// prove row padding stays untouched.
					ctxStride := blockW + 9
					got := bytes.Repeat([]byte{0xa9}, ctxStride*blockH)
					if !e.vp9NonrdCtxPredictLuma(&ctx,
						&ctx.ref[vp9dec.LastFrame], mv, filter, got,
						ctxStride) {
						t.Fatalf("%s: ctx predictor !ok", name)
					}
					for y := range blockH {
						row := got[y*ctxStride : y*ctxStride+blockW]
						wantRow := want[y*blockW : (y+1)*blockW]
						if !bytes.Equal(row, wantRow) {
							for x := range blockW {
								if row[x] != wantRow[x] {
									t.Fatalf("%s: ctx mismatch at (%d,%d): "+
										"got %d want %d", name, x, y, row[x],
										wantRow[x])
								}
							}
						}
						if y+1 < blockH {
							for _, pad := range got[y*ctxStride+blockW : (y+1)*ctxStride] {
								if pad != 0xa9 {
									t.Fatalf("%s: ctx row %d padding overwritten",
										name, y)
								}
							}
						}
					}

					// Variance wrapper parity (fast vs legacy route).
					wantVar, wantSSE, wantOK := e.vp9InterPredictionVarianceSSETo(
						inter, miRows, miCols, pos.miRow, pos.miCol, bsize,
						common.NewMv, vp9dec.LastFrame, mv, filter,
						e.blockScratch[:blockW*blockH], blockW)
					dst := make([]byte, blockW*blockH)
					gotVar, gotSSE, gotOK := e.vp9NonrdPredictVarianceSSETo(
						&ctx, inter, miRows, miCols, pos.miRow, pos.miCol,
						bsize, common.NewMv, vp9dec.LastFrame, mv, filter,
						dst, blockW)
					if gotOK != wantOK || gotVar != wantVar || gotSSE != wantSSE {
						t.Fatalf("%s: variance/SSE = %d/%d/%v, want %d/%d/%v",
							name, gotVar, gotSSE, gotOK, wantVar, wantSSE,
							wantOK)
					}
					if wantOK && !bytes.Equal(dst, want) {
						t.Fatalf("%s: variance-wrapper predictor mismatch", name)
					}

					// Chroma parity: legacy per-plane UV route vs the
					// prepared-context route, including the written live
					// recon chroma rect.
					for plane := 1; plane <= 2; plane++ {
						uvW := ctx.uvBw
						uvH := ctx.uvBh
						if !ctx.uvValid {
							pd := &e.planes[plane]
							pb := vp9dec.GetPlaneBlockSize(bsize, pd)
							if pb >= common.BlockSizes {
								continue
							}
							uvW = int(common.Num4x4BlocksWideLookup[pb]) * 4
							uvH = int(common.Num4x4BlocksHighLookup[pb]) * 4
						}
						uvX := (pos.miCol * common.MiSize) >> e.planes[plane].SubsamplingX
						uvY := (pos.miRow * common.MiSize) >> e.planes[plane].SubsamplingY
						reconUV, reconUVStride := e.vp9EncoderReconPlane(plane)
						wantUVVar, wantUVSSE, wantUVOK := e.vp9NonrdUVVariancePlaneSSE(
							inter, miRows, miCols, pos.miRow, pos.miCol,
							bsize, common.NewMv, vp9dec.LastFrame, mv,
							filter, plane)
						var wantRect []byte
						if wantUVOK {
							wantRect = make([]byte, uvW*uvH)
							vp9CopyPredRectToScratch(wantRect, reconUV,
								reconUVStride, uvX, uvY, uvW, uvH)
							// Scribble so a no-op ctx call cannot pass.
							for yy := range uvH {
								for xx := range uvW {
									reconUV[(uvY+yy)*reconUVStride+uvX+xx] = 0x33
								}
							}
						}
						gotUVVar, gotUVSSE, gotUVOK := e.vp9NonrdCtxUVVariancePlaneSSE(
							&ctx, inter, miRows, miCols, pos.miRow, pos.miCol,
							bsize, common.NewMv, vp9dec.LastFrame, mv,
							filter, plane)
						if gotUVOK != wantUVOK || gotUVVar != wantUVVar ||
							gotUVSSE != wantUVSSE {
							t.Fatalf("%s p%d: UV variance/SSE = %d/%d/%v, want %d/%d/%v",
								name, plane, gotUVVar, gotUVSSE, gotUVOK,
								wantUVVar, wantUVSSE, wantUVOK)
						}
						if wantUVOK {
							gotRect := make([]byte, uvW*uvH)
							vp9CopyPredRectToScratch(gotRect, reconUV,
								reconUVStride, uvX, uvY, uvW, uvH)
							if !bytes.Equal(gotRect, wantRect) {
								t.Fatalf("%s p%d: UV recon rect mismatch",
									name, plane)
							}
						}
					}
				}
			}
		}
	}
}

// TestVP9NonrdPredCtxAddRefRejectsScaledRef pins the fallback contract: a
// reference whose dimensions differ from the encode dimensions must not enter
// the prepared path (libvpx routes scaled references through
// vp9_is_scaled / the generic builder).
func TestVP9NonrdPredCtxAddRefRejectsScaledRef(t *testing.T) {
	const width, height = 128, 96
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height,
		CpuUsed: 8})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter := &vp9InterEncodeState{
		img:     shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 1, 1),
		ref:     &e.refFrames[0],
		allowHP: true,
	}
	var ctx vp9NonrdPredBlockCtx
	if !e.vp9NonrdPredBlockCtxInit(&ctx, inter, 0, 0, common.Block16x16) {
		t.Fatal("block ctx init failed")
	}
	savedW := e.refFrames[0].img.Width
	e.refFrames[0].img.Width = savedW / 2
	if e.vp9NonrdPredBlockCtxAddRef(&ctx, vp9dec.LastFrame, &e.refFrames[0]) {
		t.Fatal("AddRef accepted a scaled reference")
	}
	e.refFrames[0].img.Width = savedW
	if ctx.ref[vp9dec.LastFrame].valid {
		t.Fatal("scaled reference left a valid ref plane")
	}
	// The variance wrapper must fall back to the legacy route for invalid
	// ref entries and stay result-identical.
	e.prepareVP9EncoderOutputFrame(width, height)
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	mv := vp9dec.MV{Row: 3, Col: -5}
	wantVar, wantSSE, wantOK := e.vp9InterPredictionVarianceSSETo(inter,
		miRows, miCols, 0, 0, common.Block16x16, common.NewMv,
		vp9dec.LastFrame, mv, vp9dec.InterpEighttap,
		e.blockScratch[:16*16], 16)
	dst := make([]byte, 16*16)
	gotVar, gotSSE, gotOK := e.vp9NonrdPredictVarianceSSETo(&ctx, inter,
		miRows, miCols, 0, 0, common.Block16x16, common.NewMv,
		vp9dec.LastFrame, mv, vp9dec.InterpEighttap, dst, 16)
	if gotOK != wantOK || gotVar != wantVar || gotSSE != wantSSE {
		t.Fatalf("fallback variance/SSE = %d/%d/%v, want %d/%d/%v",
			gotVar, gotSSE, gotOK, wantVar, wantSSE, wantOK)
	}
}
