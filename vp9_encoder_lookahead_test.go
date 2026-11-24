package govpx

import (
	"bytes"
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func TestVP9EncoderLookaheadDelaysAndFlushes(t *testing.T) {
	const width, height = 64, 64
	firstSrc := vp9test.NewYCbCr(width, height, 96, 128, 128)
	secondSrc := vp9test.NewYCbCr(width, height, 160, 128, 128)

	delayed, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           width,
		Height:          height,
		LookaheadFrames: 2,
	})
	if err != nil {
		t.Fatalf("delayed NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	if _, err := delayed.EncodeIntoWithResult(firstSrc, dst); !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("first lookahead encode err = %v, want ErrFrameNotReady", err)
	}
	if !vp9test.EqualYCbCr(&delayed.lookahead[0].img, firstSrc, width, height) {
		t.Fatal("lookahead copied first source incorrectly")
	}
	gotFirst, err := delayed.EncodeIntoWithFlagsResult(secondSrc, dst,
		EncodeNoUpdateLast|EncodeForceAltRefFrame)
	if err != nil {
		t.Fatalf("second lookahead encode: %v", err)
	}
	if !gotFirst.KeyFrame || len(gotFirst.Data) == 0 {
		t.Fatalf("second call result = key:%t bytes:%d, want delayed first key packet",
			gotFirst.KeyFrame, len(gotFirst.Data))
	}
	gotSecond, err := delayed.FlushIntoWithResult(dst)
	if err != nil {
		t.Fatalf("FlushIntoWithResult: %v", err)
	}
	if gotSecond.KeyFrame || len(gotSecond.Data) == 0 ||
		gotSecond.RefreshFrameFlags&(1<<vp9AltRefSlot) == 0 ||
		gotSecond.RefreshFrameFlags&(1<<vp9LastRefSlot) != 0 {
		t.Fatalf("flushed packet = key:%t bytes:%d refresh:%#x, want queued alt-ref inter",
			gotSecond.KeyFrame, len(gotSecond.Data), gotSecond.RefreshFrameFlags)
	}
	if n, err := delayed.FlushInto(dst); !errors.Is(err, ErrFrameNotReady) || n != 0 {
		t.Fatalf("empty FlushInto = n:%d err:%v, want 0/ErrFrameNotReady", n, err)
	}
}

func TestVP9EncoderSetAutoAltRefValidation(t *testing.T) {
	var nilEnc *VP9Encoder
	if err := nilEnc.SetAutoAltRef(true); !errors.Is(err, ErrClosed) {
		t.Fatalf("nil SetAutoAltRef err = %v, want ErrClosed", err)
	}

	noLookahead, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder(noLookahead): %v", err)
	}
	if err := noLookahead.SetAutoAltRef(true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetAutoAltRef without lookahead err = %v, want ErrInvalidConfig", err)
	}
	if noLookahead.opts.AutoAltRef {
		t.Fatal("invalid SetAutoAltRef mutated option")
	}

	resilient, err := NewVP9Encoder(VP9EncoderOptions{
		Width: 64, Height: 64, LookaheadFrames: 4, ErrorResilient: true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(resilient): %v", err)
	}
	if err := resilient.SetAutoAltRef(true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetAutoAltRef with error-resilient err = %v, want ErrInvalidConfig", err)
	}

	frameParallel, err := NewVP9Encoder(VP9EncoderOptions{
		Width: 64, Height: 64, LookaheadFrames: 4, FrameParallelEncoderThreads: 2,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(frameParallel): %v", err)
	}
	if err := frameParallel.SetAutoAltRef(true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetAutoAltRef with frame-parallel err = %v, want ErrInvalidConfig", err)
	}

	tpl, err := NewVP9Encoder(VP9EncoderOptions{
		Width: 64, Height: 64, LookaheadFrames: 8, AutoAltRef: true, EnableTPL: true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(tpl): %v", err)
	}
	if err := tpl.SetAutoAltRef(false); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetAutoAltRef(false) with TPL err = %v, want ErrInvalidConfig", err)
	}
	if !tpl.opts.AutoAltRef {
		t.Fatal("invalid SetAutoAltRef(false) disabled AutoAltRef under TPL")
	}

	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width: 64, Height: 64, LookaheadFrames: 4, ARNRMaxFrames: 3,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(valid): %v", err)
	}
	if err := e.SetAutoAltRef(true); err != nil {
		t.Fatalf("SetAutoAltRef(true): %v", err)
	}
	if !e.opts.AutoAltRef {
		t.Fatal("SetAutoAltRef(true) did not store option")
	}
	if len(e.autoAltRefPending.img.Y) == 0 {
		t.Fatal("SetAutoAltRef(true) did not allocate pending alt-ref frame")
	}
	if len(e.vp9ARNRScratch.Y) == 0 {
		t.Fatal("SetAutoAltRef(true) did not allocate ARNR scratch")
	}
	if err := e.SetAutoAltRef(false); err != nil {
		t.Fatalf("SetAutoAltRef(false): %v", err)
	}
	if e.opts.AutoAltRef || len(e.autoAltRefPending.img.Y) != 0 {
		t.Fatalf("SetAutoAltRef(false) left state enabled=%t pendingY=%d",
			e.opts.AutoAltRef, len(e.autoAltRefPending.img.Y))
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := e.SetAutoAltRef(true); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed SetAutoAltRef err = %v, want ErrClosed", err)
	}
}

func TestVP9EncoderSetAutoAltRefRequiresDrainedLookahead(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width: 64, Height: 64, LookaheadFrames: 4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	src := vp9test.NewYCbCr(64, 64, 96, 128, 128)
	if _, err := e.EncodeIntoWithResult(src, dst); !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("queued EncodeIntoWithResult err = %v, want ErrFrameNotReady", err)
	}
	if err := e.SetAutoAltRef(true); !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("queued SetAutoAltRef err = %v, want ErrFrameNotReady", err)
	}
	if e.opts.AutoAltRef {
		t.Fatal("queued SetAutoAltRef mutated option")
	}
}

func TestVP9EncoderSetAutoAltRefEmitsHiddenAltRef(t *testing.T) {
	const width, height = 64, 64
	const frames = 6
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		Deadline:           DeadlineRealtime,
		CpuUsed:            4,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  300,
		LookaheadFrames:    4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetAutoAltRef(true); err != nil {
		t.Fatalf("SetAutoAltRef(true): %v", err)
	}

	dst := make([]byte, 65536)
	results := make([]VP9EncodeResult, 0, frames+1)
	for frame := range frames {
		src := vp9test.NewYCbCr(width, height, uint8(80+frame*17), 128, 128)
		result, err := e.EncodeIntoWithResult(src, dst)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		results = append(results, result)
	}
	for {
		result, err := e.FlushIntoWithResult(dst)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushIntoWithResult: %v", err)
		}
		results = append(results, result)
	}

	hiddenCount := 0
	for _, result := range results {
		if result.ShowFrame {
			continue
		}
		hiddenCount++
		if result.KeyFrame || result.Dropped || len(result.Data) == 0 ||
			result.RefreshFrameFlags != 1<<vp9AltRefSlot {
			t.Fatalf("hidden packet = key:%t dropped:%t bytes:%d refresh:%#x, want inter ALTREF refresh",
				result.KeyFrame, result.Dropped, len(result.Data),
				result.RefreshFrameFlags)
		}
	}
	if hiddenCount != 1 {
		t.Fatalf("hidden auto-alt-ref packets = %d, want 1", hiddenCount)
	}
}

func TestVP9EncoderAutoAltRefLookaheadEmitsHiddenAltRef(t *testing.T) {
	const width, height = 64, 64
	const frames = 6
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		Deadline:           DeadlineRealtime,
		CpuUsed:            4,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  300,
		LookaheadFrames:    4,
		AutoAltRef:         true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}

	dst := make([]byte, 65536)
	results := make([]VP9EncodeResult, 0, frames+1)
	packets := make([][]byte, 0, frames+1)
	for frame := range frames {
		src := vp9test.NewYCbCr(width, height, uint8(80+frame*17), 128, 128)
		result, err := e.EncodeIntoWithResult(src, dst)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		results = append(results, result)
		packets = append(packets, append([]byte(nil), result.Data...))
	}
	for {
		result, err := e.FlushIntoWithResult(dst)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushIntoWithResult: %v", err)
		}
		results = append(results, result)
		packets = append(packets, append([]byte(nil), result.Data...))
	}
	if got, want := len(results), frames+1; got != want {
		t.Fatalf("auto-alt-ref packets = %d, want %d", got, want)
	}

	hiddenIndex := -1
	for i := range results {
		if !results[i].ShowFrame {
			if hiddenIndex >= 0 {
				t.Fatalf("multiple hidden packets: first=%d second=%d", hiddenIndex, i)
			}
			hiddenIndex = i
		}
	}
	if hiddenIndex < 0 {
		t.Fatal("auto-alt-ref emitted no hidden packet")
	}
	hidden := results[hiddenIndex]
	if hidden.KeyFrame || hidden.Dropped || len(hidden.Data) == 0 ||
		hidden.RefreshFrameFlags != 1<<vp9AltRefSlot {
		t.Fatalf("hidden packet = key:%t dropped:%t bytes:%d refresh:%#x, want inter ALTREF refresh",
			hidden.KeyFrame, hidden.Dropped, len(hidden.Data), hidden.RefreshFrameFlags)
	}
	if hiddenIndex == 0 || !results[0].KeyFrame {
		t.Fatalf("hidden index/key ordering = index:%d firstKey:%t, want hidden after first key",
			hiddenIndex, results[0].KeyFrame)
	}
	for i := range results {
		if i == hiddenIndex {
			continue
		}
		if !results[i].ShowFrame || results[i].Dropped || len(results[i].Data) == 0 {
			t.Fatalf("visible packet %d = show:%t dropped:%t bytes:%d",
				i, results[i].ShowFrame, results[i].Dropped, len(results[i].Data))
		}
	}

	dec, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	visible := 0
	for i, packet := range packets {
		if err := dec.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if i == hiddenIndex {
			if _, ok := dec.NextFrame(); ok {
				t.Fatal("NextFrame returned visible output after hidden ALTREF")
			}
			if info, ok := dec.LastFrameInfo(); !ok || info.ShowFrame ||
				info.RefreshFrameFlags != 1<<vp9AltRefSlot {
				t.Fatalf("LastFrameInfo after hidden = %+v ok=%t, want hidden ALTREF refresh",
					info, ok)
			}
			continue
		}
		if _, ok := dec.NextFrame(); !ok {
			t.Fatalf("NextFrame packet %d returned !ok", i)
		}
		visible++
	}
	if visible != frames {
		t.Fatalf("visible decoded frames = %d, want %d", visible, frames)
	}
}

func TestVP9EncoderAutoAltRefPublicQDoesNotEmitHiddenAltRef(t *testing.T) {
	const width, height = 64, 64
	const frames = 6
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           width,
		Height:          height,
		LookaheadFrames: 4,
		AutoAltRef:      true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}

	dst := make([]byte, 65536)
	results := make([]VP9EncodeResult, 0, frames)
	for frame := range frames {
		src := vp9test.NewYCbCr(width, height, uint8(80+frame*17), 128, 128)
		result, err := e.EncodeIntoWithResult(src, dst)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		results = append(results, result)
	}
	for {
		result, err := e.FlushIntoWithResult(dst)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushIntoWithResult: %v", err)
		}
		results = append(results, result)
	}
	if got, want := len(results), frames; got != want {
		t.Fatalf("auto-alt-ref public-Q packets = %d, want %d", got, want)
	}
	for i := range results {
		if !results[i].ShowFrame {
			t.Fatalf("public-Q packet %d hidden with refresh=%#x; libvpx source_alt_ref_pending stays false",
				i, results[i].RefreshFrameFlags)
		}
	}
}

func TestVP9EncoderAutoAltRefARNRFiltersHiddenSource(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           width,
		Height:          height,
		Deadline:        DeadlineGoodQuality,
		LookaheadFrames: 4,
		AutoAltRef:      true,
		ARNRMaxFrames:   5,
		ARNRStrength:    6,
		ARNRType:        1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	for i := range 4 {
		src := vp9test.NewYCbCr(width, height, uint8(100+i*4), 128, 128)
		if err := e.pushVP9Lookahead(src, 0); err != nil {
			t.Fatalf("pushVP9Lookahead %d: %v", i, err)
		}
	}
	future, ok := e.newestVP9LookaheadEntry()
	if !ok {
		t.Fatal("newestVP9LookaheadEntry returned !ok")
	}
	before := append([]byte(nil), future.img.Y...)
	if !e.applyVP9ARNRFilter(future) {
		t.Fatal("applyVP9ARNRFilter returned false")
	}
	if bytes.Equal(e.vp9ARNRScratch.Y, future.img.Y) {
		t.Fatal("ARNR scratch luma matches unfiltered future source")
	}
	if !bytes.Equal(before, future.img.Y) {
		t.Fatal("ARNR mutated queued future source")
	}
}

func TestVP9EncoderRealtimeAutoAltRefSkipsARNRFilter(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		Deadline:           DeadlineRealtime,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  300,
		LookaheadFrames:    4,
		AutoAltRef:         true,
		ARNRMaxFrames:      5,
		ARNRStrength:       6,
		ARNRType:           3,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	for i := range 4 {
		src := vp9test.NewYCbCr(width, height, uint8(100+i*4), 128, 128)
		if err := e.pushVP9Lookahead(src, 0); err != nil {
			t.Fatalf("pushVP9Lookahead %d: %v", i, err)
		}
	}
	future, ok := e.newestVP9LookaheadEntry()
	if !ok {
		t.Fatal("newestVP9LookaheadEntry returned !ok")
	}
	if e.applyVP9ARNRFilter(future) {
		t.Fatal("realtime ARNR filter ran, want libvpx-compatible no-op")
	}
}

func TestVP9SourceAltRefOverlayGateIncludesFilteredARNR(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  300,
		LookaheadFrames:    4,
		AutoAltRef:         true,
		ARNRMaxFrames:      5,
		ARNRStrength:       3,
		ARNRType:           3,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	inter := &vp9InterEncodeState{isSrcFrameAltRef: true}
	if !e.vp9OnePassVBRSourceAltRefOverlay(inter) {
		t.Fatal("filtered ARNR source-alt-ref overlay gate = false, want true")
	}
	e.opts.ARNRMaxFrames = 0
	if !e.vp9OnePassVBRSourceAltRefOverlay(inter) {
		t.Fatal("unfiltered source-alt-ref overlay gate = false, want true")
	}
	e.opts.RateControlModeSet = false
	if e.vp9OnePassVBRSourceAltRefOverlay(inter) {
		t.Fatal("public-Q source-alt-ref overlay gate = true, want false")
	}
}

func TestVP9EncoderAutoAltRefARNRSteadyStateAlloc(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           width,
		Height:          height,
		Deadline:        DeadlineGoodQuality,
		LookaheadFrames: 4,
		AutoAltRef:      true,
		ARNRMaxFrames:   5,
		ARNRStrength:    6,
		ARNRType:        1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	for i := range 4 {
		src := vp9test.NewYCbCr(width, height, uint8(100+i*4), 128, 128)
		if err := e.pushVP9Lookahead(src, 0); err != nil {
			t.Fatalf("pushVP9Lookahead %d: %v", i, err)
		}
	}
	future, ok := e.newestVP9LookaheadEntry()
	if !ok {
		t.Fatal("newestVP9LookaheadEntry returned !ok")
	}
	if !e.applyVP9ARNRFilter(future) {
		t.Fatal("warm applyVP9ARNRFilter returned false")
	}
	allocs := testing.AllocsPerRun(10, func() {
		if !e.applyVP9ARNRFilter(future) {
			t.Fatal("applyVP9ARNRFilter returned false")
		}
	})
	if allocs != 0 {
		t.Fatalf("VP9 ARNR steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9EncoderLookaheadQueuedFrameBlocksResize(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           width,
		Height:          height,
		LookaheadFrames: 2,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	if _, err := e.EncodeIntoWithResult(
		vp9test.NewYCbCr(width, height, 96, 128, 128), dst); !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("lookahead fill err = %v, want ErrFrameNotReady", err)
	}
	err = e.SetRealtimeTarget(RealtimeTarget{Width: 96, Height: 64})
	if !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("queued resize err = %v, want ErrFrameNotReady", err)
	}
}
