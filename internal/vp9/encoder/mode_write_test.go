package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestWriteIntraModeRoundTrip walks every PredictionMode leaf in
// IntraModeTree through WriteIntraMode and confirms ReadIntraMode
// recovers the original byte.
func TestWriteIntraModeRoundTrip(t *testing.T) {
	probs := make([]uint8, 9)
	for i := range probs {
		probs[i] = 128
	}
	for mode := common.DcPred; mode <= common.TmPred; mode++ {
		buf := make([]byte, 32)
		var bw bitstream.Writer
		bw.Start(buf)
		WriteIntraMode(&bw, mode, probs)
		size, err := bw.Stop()
		if err != nil {
			t.Fatalf("mode %d: Stop: %v", mode, err)
		}
		var r bitstream.Reader
		r.Init(buf[:size])
		got := vp9dec.ReadIntraMode(&r, probs)
		if got != mode {
			t.Errorf("mode %d round-tripped to %d", mode, got)
		}
	}
}

// TestWriteInterModeRoundTrip exercises the four inter modes
// (NEARESTMV..NEWMV).
func TestWriteInterModeRoundTrip(t *testing.T) {
	var probs [common.InterModes - 1]uint8
	for i := range probs {
		probs[i] = 128
	}
	for mode := common.NearestMv; mode <= common.NewMv; mode++ {
		buf := make([]byte, 32)
		var bw bitstream.Writer
		bw.Start(buf)
		WriteInterMode(&bw, mode, probs[:])
		size, _ := bw.Stop()
		var r bitstream.Reader
		r.Init(buf[:size])
		got := vp9dec.ReadInterMode(&r, probs)
		if got != mode {
			t.Errorf("mode %d round-tripped to %d", mode, got)
		}
	}
}

// TestWritePartitionRoundTrip walks every partition type in the
// has-rows-and-cols case.
func TestWritePartitionRoundTrip(t *testing.T) {
	probs := []uint8{128, 128, 128}
	for p := common.PartitionType(0); p < common.PartitionTypes; p++ {
		buf := make([]byte, 32)
		var bw bitstream.Writer
		bw.Start(buf)
		WritePartition(&bw, p, probs, true, true)
		size, _ := bw.Stop()
		var r bitstream.Reader
		r.Init(buf[:size])
		got := vp9dec.ReadPartition(&r, probs, true, true)
		if got != p {
			t.Errorf("part %d round-tripped to %d", p, got)
		}
	}
}

// TestWriteSelectedTxSizeRoundTrip drives the cascade for a
// max=Tx32x32 frame and confirms every TxSize round-trips.
func TestWriteSelectedTxSizeRoundTrip(t *testing.T) {
	probs := []uint8{128, 128, 128}
	for tx := common.Tx4x4; tx <= common.Tx32x32; tx++ {
		buf := make([]byte, 32)
		var bw bitstream.Writer
		bw.Start(buf)
		WriteSelectedTxSize(&bw, tx, common.Tx32x32, probs)
		size, _ := bw.Stop()
		var r bitstream.Reader
		r.Init(buf[:size])
		got := vp9dec.ReadSelectedTxSize(&r, common.Tx32x32, probs)
		if got != tx {
			t.Errorf("tx %d round-tripped to %d", tx, got)
		}
	}
}
