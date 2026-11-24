package govpx

import "testing"

func TestVP9DecoderPastIndependenceClearsSegmentationMaps(t *testing.T) {
	var d VP9Decoder
	d.ensureVP9DecoderModeBuffers(2, 3)
	for i := range d.segMap {
		d.segMap[i] = 7
	}
	for i := range d.lastSegMap {
		d.lastSegMap[i] = 5
	}

	d.resetVP9SegmentationMapsForPastIndependence()

	for i, v := range d.segMap {
		if v != 0 {
			t.Fatalf("segMap[%d] = %d, want 0", i, v)
		}
	}
	for i, v := range d.lastSegMap {
		if v != 0 {
			t.Fatalf("lastSegMap[%d] = %d, want 0", i, v)
		}
	}
}
