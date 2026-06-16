package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9WebRTCPacketizerTemporalDropsPassRefFinder(t *testing.T) {
	for _, tc := range []struct {
		name        string
		dropFrame   int
		droppedTLID int
	}{
		{name: "drop-tl2", dropFrame: 1, droppedTLID: 2},
		{name: "drop-tl1", dropFrame: 2, droppedTLID: 1},
		{name: "drop-tl0", dropFrame: 4, droppedTLID: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const width, height = 64, 64
			wantRecoveryKey := tc.droppedTLID < 2
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
			refFinder := newVP9WebRTCPlainRefFinderForTest()
			drops := 0
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
				pictureID := packetizer.PictureID()
				payloads, sent, err := packetizer.Packetize(result, 500)
				if err != nil {
					t.Fatalf("Packetize[%d]: %v", frame, err)
				}
				if result.Dropped {
					drops++
					if result.TemporalLayerID != tc.droppedTLID {
						t.Fatalf("dropped frame %d temporal layer = %d, want %d",
							frame, result.TemporalLayerID, tc.droppedTLID)
					}
					if got := packetizer.NeedsKeyFrame(); got != wantRecoveryKey {
						t.Fatalf("NeedsKeyFrame after dropped frame %d = %t, want %t",
							frame, got, wantRecoveryKey)
					}
					if sent || len(payloads) != 0 {
						t.Fatalf("dropped Packetize[%d] = payloads:%d sent:%t",
							frame, len(payloads), sent)
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
					t.Fatalf("non-dropped frame %d reported unsent", frame)
				}
				refFinder.accept(t, frame, payloads, pictureID)
			}
			if drops != 1 {
				t.Fatalf("drops = %d, want 1", drops)
			}
		})
	}
}

type vp9WebRTCPlainRefFinderForTest struct {
	gof              *vp9WebRTCPlainGOFForTest
	available        map[uint16]bool
	upSwitch         map[uint16]uint8
	missingByTID     [8]map[uint16]bool
	lastUnwrappedPID int
	havePID          bool
}

type vp9WebRTCPlainGOFForTest struct {
	groups        []VP9RTPPictureGroup
	pidStart      uint16
	lastPictureID uint16
}

func newVP9WebRTCPlainRefFinderForTest() *vp9WebRTCPlainRefFinderForTest {
	f := &vp9WebRTCPlainRefFinderForTest{
		available: make(map[uint16]bool),
		upSwitch:  make(map[uint16]uint8),
	}
	for i := range f.missingByTID {
		f.missingByTID[i] = make(map[uint16]bool)
	}
	return f
}

func (f *vp9WebRTCPlainRefFinderForTest) accept(
	t *testing.T,
	frame int,
	payloads []RTPPayloadFragment,
	pictureID uint16,
) {
	t.Helper()
	desc, err := firstVP9WebRTCStartDescriptorForTest(payloads)
	if err != nil {
		t.Fatalf("frame %d descriptor: %v", frame, err)
	}
	if desc.PictureID != pictureID {
		t.Fatalf("frame %d PictureID = %d, want %d",
			frame, desc.PictureID, pictureID)
	}
	if !desc.LayerIndicesPresent {
		t.Fatalf("frame %d missing temporal metadata", frame)
	}
	if desc.ScalabilityStructurePresent && desc.TemporalID == 0 {
		groups := desc.ScalabilityStructure.PictureGroups
		if !desc.ScalabilityStructure.PictureGroupPresent || len(groups) == 0 {
			groups = []VP9RTPPictureGroup{{TemporalID: 0}}
		}
		f.gof = &vp9WebRTCPlainGOFForTest{
			groups:        append([]VP9RTPPictureGroup(nil), groups...),
			pidStart:      desc.PictureID,
			lastPictureID: desc.PictureID,
		}
		f.markReceived(desc.PictureID, f.gof)
		f.available[desc.PictureID] = true
		return
	}
	if f.gof == nil {
		t.Fatalf("frame %d reached ref finder before SS", frame)
	}
	f.markReceived(desc.PictureID, f.gof)
	gofIdx := vp9WebRTCPlainForwardDiff(f.gof.pidStart, desc.PictureID) %
		len(f.gof.groups)
	group := f.gof.groups[gofIdx]
	for tid := uint8(0); tid < group.TemporalID; tid++ {
		for missing := range f.missingByTID[tid] {
			if vp9WebRTCPlainAheadOf(missing,
				vp9WebRTCPlainPictureIDSub(desc.PictureID, 1)) &&
				vp9WebRTCPlainAheadOf(desc.PictureID, missing) {
				t.Fatalf("frame %d would be stashed by VP9 ref finder: missing pid %d tid %d before pid %d",
					frame, missing, tid, desc.PictureID)
			}
		}
	}
	if desc.SwitchingUpPoint {
		f.upSwitch[desc.PictureID] = desc.TemporalID
	}
	if desc.InterPicturePredicted {
		for i := 0; i < group.ReferenceIndexCount; i++ {
			refPID := vp9WebRTCPlainPictureIDSub(desc.PictureID,
				group.ReferenceIndices[i])
			if f.upSwitchInInterval(desc.PictureID, desc.TemporalID, refPID) {
				continue
			}
			if !f.available[refPID] {
				t.Fatalf("frame %d requires unavailable GOF ref pid %d for pid %d",
					frame, refPID, desc.PictureID)
			}
		}
	}
	f.available[desc.PictureID] = true
}

func firstVP9WebRTCStartDescriptorForTest(
	payloads []RTPPayloadFragment,
) (VP9RTPPayloadDescriptor, error) {
	for _, payload := range payloads {
		desc, _, err := ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			return VP9RTPPayloadDescriptor{}, err
		}
		if desc.StartOfFrame {
			return desc, nil
		}
	}
	return VP9RTPPayloadDescriptor{}, ErrInvalidVP9Data
}

func (f *vp9WebRTCPlainRefFinderForTest) markReceived(
	pictureID uint16,
	gof *vp9WebRTCPlainGOFForTest,
) {
	if vp9WebRTCPlainAheadOf(pictureID, gof.lastPictureID) {
		gofIdx := vp9WebRTCPlainForwardDiff(gof.pidStart,
			gof.lastPictureID) % len(gof.groups)
		next := NextVP9RTPPictureID(gof.lastPictureID)
		for next != pictureID {
			gofIdx = (gofIdx + 1) % len(gof.groups)
			tid := gof.groups[gofIdx].TemporalID
			if int(tid) < len(f.missingByTID) {
				f.missingByTID[tid][next] = true
			}
			next = NextVP9RTPPictureID(next)
		}
		gof.lastPictureID = pictureID
		return
	}
	gofIdx := vp9WebRTCPlainForwardDiff(gof.pidStart, pictureID) %
		len(gof.groups)
	tid := gof.groups[gofIdx].TemporalID
	if int(tid) < len(f.missingByTID) {
		delete(f.missingByTID[tid], pictureID)
	}
}

func (f *vp9WebRTCPlainRefFinderForTest) upSwitchInInterval(
	pictureID uint16,
	temporalID uint8,
	refPictureID uint16,
) bool {
	for upSwitchID, upSwitchTemporalID := range f.upSwitch {
		if vp9WebRTCPlainAheadOf(upSwitchID, refPictureID) &&
			vp9WebRTCPlainAheadOf(pictureID, upSwitchID) &&
			upSwitchTemporalID < temporalID {
			return true
		}
	}
	return false
}

func vp9WebRTCPlainPictureIDSub(id uint16, delta uint8) uint16 {
	return (id - uint16(delta)) & VP9RTPPictureID15BitMask
}

func vp9WebRTCPlainForwardDiff(from uint16, to uint16) int {
	return int((to - from) & VP9RTPPictureID15BitMask)
}

func vp9WebRTCPlainAheadOf(a uint16, b uint16) bool {
	if a == b {
		return false
	}
	diff := (a - b) & VP9RTPPictureID15BitMask
	return diff < (VP9RTPPictureID15BitMask+1)/2
}
