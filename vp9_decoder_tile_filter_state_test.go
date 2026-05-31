package govpx

import (
	"errors"
	"testing"
)

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
				t.Fatalf("%s set/value = %v/%d, want true/%d", tc.fieldName,
					ok, got, tc.value)
			}
			if err := tc.set(d, tc.clear); err != nil {
				t.Fatalf("set(%d): %v", tc.clear, err)
			}
			if ok, got := tc.got(d); ok || got != 0 {
				t.Fatalf("%s set/value = %v/%d, want false/0", tc.fieldName,
					ok, got)
			}
			if err := tc.set(d, vp9DecoderMaxTileFilter+1); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("oversize err = %v, want ErrInvalidConfig", err)
			}
		})
	}
}
