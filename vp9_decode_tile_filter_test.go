package govpx

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestVP9DecoderRejectsOutOfRangeDecodeTileFilters(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*VP9DecoderOptions)
	}{
		{name: "row out of range", mut: func(o *VP9DecoderOptions) {
			o.DecodeTileRowSet = true
			o.DecodeTileRow = vp9DecoderMaxTileFilter + 1
		}},
		{name: "col out of range", mut: func(o *VP9DecoderOptions) {
			o.DecodeTileColSet = true
			o.DecodeTileCol = vp9DecoderMaxTileFilter + 1
		}},
		{name: "row below disable sentinel", mut: func(o *VP9DecoderOptions) {
			o.DecodeTileRowSet = true
			o.DecodeTileRow = -2
		}},
		{name: "col below disable sentinel", mut: func(o *VP9DecoderOptions) {
			o.DecodeTileColSet = true
			o.DecodeTileCol = -2
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var opts VP9DecoderOptions
			tc.mut(&opts)
			if _, err := NewVP9Decoder(opts); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("NewVP9Decoder err = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestVP9DecoderSetDecodeTileAcceptsNegativeAsClear(t *testing.T) {
	cases := []struct {
		name      string
		set       func(*VP9Decoder, int) error
		got       func(*VP9Decoder) (bool, int)
		value     int
		clear     int
		fieldName string
	}{
		{
			name:      "row",
			set:       (*VP9Decoder).SetDecodeTileRow,
			got:       func(d *VP9Decoder) (bool, int) { return d.opts.DecodeTileRowSet, d.opts.DecodeTileRow },
			value:     3,
			clear:     -1,
			fieldName: "DecodeTileRow",
		},
		{
			name:      "col",
			set:       (*VP9Decoder).SetDecodeTileCol,
			got:       func(d *VP9Decoder) (bool, int) { return d.opts.DecodeTileColSet, d.opts.DecodeTileCol },
			value:     2,
			clear:     -7,
			fieldName: "DecodeTileCol",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := NewVP9Decoder(VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP9Decoder: %v", err)
			}
			defer d.Close()
			if err := tc.set(d, tc.value); err != nil {
				t.Fatalf("set(%d): %v", tc.value, err)
			}
			if ok, got := tc.got(d); !ok || got != tc.value {
				t.Fatalf("%s set/value = %v/%d, want true/%d", tc.fieldName, ok, got, tc.value)
			}
			if err := tc.set(d, tc.clear); err != nil {
				t.Fatalf("set(%d): %v", tc.clear, err)
			}
			if ok, got := tc.got(d); ok || got != 0 {
				t.Fatalf("%s set/value = %v/%d, want false/0", tc.fieldName, ok, got)
			}
			if err := tc.set(d, vp9DecoderMaxTileFilter+1); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("oversize err = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

// TestVP9DecoderDecodeTileColFilterMasksOtherTiles drives a multi-tile
// keyframe through the public decoder with the DecodeTileCol filter
// pinned to a non-zero tile. The masked tile region must retain the
// frame buffer's neutral fill while the selected tile decodes to the
// expected DC-pred constant.
func TestVP9DecoderDecodeTileColFilterMasksOtherTiles(t *testing.T) {
	const log2TileCols = 1
	packet := vp9MultiTileStubPacketForTest(t, 1024, 64, log2TileCols)

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	if err := d.SetDecodeTileCol(1); err != nil {
		t.Fatalf("SetDecodeTileCol(1): %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after filtered keyframe")
	}
	if frame.Width != 1024 || frame.Height != 64 {
		t.Fatalf("frame dims = %dx%d, want 1024x64", frame.Width, frame.Height)
	}
	// Masked tile column 0 covers x in [0, 512). The buffer is
	// pre-filled with 128 by prepareVP9OutputFrame, so masked pixels
	// remain at 128.
	for y := 0; y < frame.Height; y++ {
		for x := range 512 {
			if got := frame.Y[y*frame.YStride+x]; got != 128 {
				t.Fatalf("masked tile col 0 Y[%d,%d] = %d, want 128", y, x, got)
			}
		}
	}
	// Selected tile column 1 covers x in [512, 1024) and should hold
	// the DC predictor's filled luma value (128 for an all-zero
	// residual DC-pred keyframe with no above row).
	for y := 0; y < frame.Height; y++ {
		for x := 512; x < 1024; x++ {
			if got := frame.Y[y*frame.YStride+x]; got != 128 {
				t.Fatalf("selected tile col 1 Y[%d,%d] = %d, want 128", y, x, got)
			}
		}
	}
}

func TestVP9DecoderDecodeTileColFilterSelectsLogicalTileWithInvert(t *testing.T) {
	packet := vp9MultiTileModePacketForTest(t, 1024, 64, 1,
		[]common.PredictionMode{common.DcPred, common.VPred})

	for _, invert := range []bool{false, true} {
		d, err := NewVP9Decoder(VP9DecoderOptions{
			InvertTileDecodeOrder: invert,
		})
		if err != nil {
			t.Fatalf("invert=%v NewVP9Decoder: %v", invert, err)
		}
		if err := d.SetDecodeTileCol(1); err != nil {
			t.Fatalf("invert=%v SetDecodeTileCol(1): %v", invert, err)
		}
		if err := d.Decode(packet); err != nil {
			t.Fatalf("invert=%v Decode: %v", invert, err)
		}
		frame, ok := d.NextFrame()
		if !ok {
			t.Fatalf("invert=%v NextFrame returned !ok", invert)
		}
		if err := d.Close(); err != nil {
			t.Fatalf("invert=%v Close: %v", invert, err)
		}

		for y := 0; y < frame.Height; y++ {
			for x := range 512 {
				if got := frame.Y[y*frame.YStride+x]; got != 128 {
					t.Fatalf("invert=%v masked tile col 0 Y[%d,%d] = %d, want 128",
						invert, y, x, got)
				}
			}
			for x := 512; x < 1024; x++ {
				if got := frame.Y[y*frame.YStride+x]; got != 127 {
					t.Fatalf("invert=%v selected tile col 1 Y[%d,%d] = %d, want 127",
						invert, y, x, got)
				}
			}
		}
	}
}

// TestVP9DecoderDecodeTileColFilterIgnoredForSingleTileFrames confirms
// the filter is a no-op when the frame's tile grid has a single column.
func TestVP9DecoderDecodeTileColFilterIgnoredForSingleTileFrames(t *testing.T) {
	packet := vp9MultiTileStubPacketForTest(t, 1024, 64, 0)
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	if err := d.SetDecodeTileCol(7); err != nil {
		t.Fatalf("SetDecodeTileCol(7): %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after single-tile frame")
	}
	assertVP9NeutralFrame(t, frame, 1024, 64)
}
