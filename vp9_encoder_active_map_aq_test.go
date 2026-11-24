package govpx

import (
	"bytes"
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

func TestVP9EncoderSetActiveMapValidationAndCopy(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	activeMap := make([]uint8, rows*cols)
	for i := range activeMap {
		activeMap[i] = 1
	}
	activeMap[0] = 0
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	if !e.activeMapEnabled || e.activeMapMiRows != 8 || e.activeMapMiCols != 8 {
		t.Fatalf("active-map state = enabled:%t mi:%dx%d, want true 8x8",
			e.activeMapEnabled, e.activeMapMiRows, e.activeMapMiCols)
	}
	for _, idx := range []int{0, 1, 8, 9} {
		if e.activeMap[idx] != vp9ActiveMapSegmentInactive {
			t.Fatalf("expanded inactive map[%d] = %d, want %d",
				idx, e.activeMap[idx], vp9ActiveMapSegmentInactive)
		}
	}
	if e.activeMap[2] != vp9ActiveMapSegmentActive {
		t.Fatalf("expanded active map[2] = %d, want %d",
			e.activeMap[2], vp9ActiveMapSegmentActive)
	}
	activeMap[0] = 1
	if e.activeMap[0] != vp9ActiveMapSegmentInactive {
		t.Fatal("SetActiveMap kept caller slice instead of copying")
	}

	oldMap := append([]uint8(nil), e.activeMap...)
	oldRows, oldCols := e.activeMapMiRows, e.activeMapMiCols
	if err := e.SetActiveMap(activeMap, rows+1, cols); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("bad rows SetActiveMap err = %v, want ErrInvalidConfig", err)
	}
	if !e.activeMapEnabled || e.activeMapMiRows != oldRows ||
		e.activeMapMiCols != oldCols || !bytes.Equal(e.activeMap, oldMap) {
		t.Fatal("invalid SetActiveMap mutated encoder state")
	}
	if err := e.SetActiveMap(activeMap[:len(activeMap)-1], rows, cols); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("short map SetActiveMap err = %v, want ErrInvalidConfig", err)
	}
	if !e.activeMapEnabled || !bytes.Equal(e.activeMap, oldMap) {
		t.Fatal("short SetActiveMap mutated encoder state")
	}
	if err := e.SetActiveMap(nil, 0, 0); err != nil {
		t.Fatalf("disable SetActiveMap: %v", err)
	}
	if e.activeMapEnabled {
		t.Fatal("SetActiveMap(nil) did not disable active map")
	}
}

func TestVP9EncoderActiveMapInterBlocksUseSkipSegment(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keyPacket, err := e.Encode(vp9test.NewYCbCr(width, height, 64, 128, 128))
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	keyHeader, _ := vp9test.ParseHeader(t, keyPacket)
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	activeMap := make([]uint8, rows*cols)
	for i := range activeMap {
		activeMap[i] = 1
	}
	activeMap[0] = 0
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	interPacket, err := e.Encode(vp9test.NewYCbCr(width, height, 180, 128, 128))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	var br vp9dec.BitReader
	br.Init(interPacket)
	header, err := vp9dec.ReadUncompressedHeader(&br, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	if !header.Seg.Enabled || !header.Seg.UpdateMap || !header.Seg.UpdateData {
		t.Fatalf("active-map segmentation header = enabled:%t updateMap:%t updateData:%t, want all true",
			header.Seg.Enabled, header.Seg.UpdateMap, header.Seg.UpdateData)
	}
	if !vp9dec.SegFeatureActive(&header.Seg, int(vp9ActiveMapSegmentInactive), vp9dec.SegLvlSkip) {
		t.Fatalf("inactive segment %d missing SEG_LVL_SKIP", vp9ActiveMapSegmentInactive)
	}
	if got := header.Seg.FeatureData[vp9ActiveMapSegmentInactive][vp9dec.SegLvlAltLf]; got != -vp9dec.MaxLoopFilter {
		t.Fatalf("inactive segment alt-lf = %d, want %d",
			got, -vp9dec.MaxLoopFilter)
	}

	miCols := (width + 7) >> 3
	for _, rc := range [][2]int{{0, 0}, {0, 1}, {1, 0}, {1, 1}} {
		mi := e.miGrid[rc[0]*miCols+rc[1]]
		if mi.SegmentID != vp9ActiveMapSegmentInactive || mi.Skip != 1 ||
			mi.Mode != common.ZeroMv ||
			mi.RefFrame != [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame} {
			t.Fatalf("inactive mi[%d,%d] = seg:%d skip:%d mode:%d refs:%v, want inactive skip LAST/ZEROMV",
				rc[0], rc[1], mi.SegmentID, mi.Skip, mi.Mode, mi.RefFrame)
		}
	}
	if got := e.miGrid[2].SegmentID; got != vp9ActiveMapSegmentActive {
		t.Fatalf("active mi[0,2] segment = %d, want %d",
			got, vp9ActiveMapSegmentActive)
	}
}

func TestVP9EncoderActiveMapConstant320ChoosesTemporalPredProbs(t *testing.T) {
	const width, height = 320, 180
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		MinQuantizer: 20,
		MaxQuantizer: 20,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keyPacket, err := e.Encode(vp9test.NewYCbCr(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	keyHeader, _ := vp9test.ParseHeader(t, keyPacket)
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	activeMap := make([]uint8, rows*cols)
	for row := range rows {
		for col := range cols {
			if (row+col)&1 == 0 {
				activeMap[row*cols+col] = 1
			}
		}
	}
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	interPacket, err := e.Encode(vp9test.NewYCbCr(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	var br vp9dec.BitReader
	br.Init(interPacket)
	header, err := vp9dec.ReadUncompressedHeader(&br, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	if !header.Seg.TemporalUpdate {
		t.Fatal("active-map constant inter did not use temporal segment prediction")
	}
	for i, prob := range header.Seg.PredProbs {
		if prob != 1 {
			t.Fatalf("active-map constant pred prob[%d] = %d, want 1", i, prob)
		}
	}
}

func TestVP9EncoderActiveMapUnchangedInactiveBlocksStayBaseSegment(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keyPacket, err := e.Encode(vp9test.NewYCbCr(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	keyHeader, _ := vp9test.ParseHeader(t, keyPacket)
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	activeMap := make([]uint8, rows*cols)
	for i := range activeMap {
		activeMap[i] = 1
	}
	activeMap[0] = 0
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	interPacket, err := e.Encode(vp9test.NewYCbCr(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode unchanged inter: %v", err)
	}
	var br vp9dec.BitReader
	br.Init(interPacket)
	header, err := vp9dec.ReadUncompressedHeader(&br, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	if !header.Seg.Enabled || !header.Seg.UpdateMap || !header.Seg.UpdateData ||
		!header.Seg.TemporalUpdate {
		t.Fatalf("active-map header = enabled:%t updateMap:%t updateData:%t temporal:%t, want all true",
			header.Seg.Enabled, header.Seg.UpdateMap, header.Seg.UpdateData,
			header.Seg.TemporalUpdate)
	}

	miCols := (width + 7) >> 3
	for _, rc := range [][2]int{{0, 0}, {0, 1}, {1, 0}, {1, 1}} {
		mi := e.miGrid[rc[0]*miCols+rc[1]]
		if mi.SegmentID != vp9ActiveMapSegmentActive || mi.SegIDPredicted != 1 ||
			mi.Skip != 1 {
			t.Fatalf("unchanged inactive mi[%d,%d] = seg:%d pred:%d skip:%d, want base predicted skip",
				rc[0], rc[1], mi.SegmentID, mi.SegIDPredicted, mi.Skip)
		}
	}

	steadyPacket, err := e.Encode(vp9test.NewYCbCr(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode steady inter: %v", err)
	}
	br = vp9dec.BitReader{}
	br.Init(steadyPacket)
	steadyHeader, err := vp9dec.ReadUncompressedHeader(&br, &header,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader steady inter: %v", err)
	}
	if !steadyHeader.Seg.Enabled || steadyHeader.Seg.UpdateMap ||
		steadyHeader.Seg.UpdateData {
		t.Fatalf("steady active-map header = enabled:%t updateMap:%t updateData:%t, want enabled with no updates",
			steadyHeader.Seg.Enabled, steadyHeader.Seg.UpdateMap,
			steadyHeader.Seg.UpdateData)
	}
}

func TestVP9EncoderSetActiveMapDisabledByRuntimeResize(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	rows := encoderMacroblockRows(64)
	cols := encoderMacroblockCols(64)
	activeMap := make([]uint8, rows*cols)
	for i := range activeMap {
		activeMap[i] = 1
	}
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	roi := ROIMap{
		Enabled:   true,
		Rows:      (64 + 7) >> 3,
		Cols:      (64 + 7) >> 3,
		SegmentID: make([]uint8, ((64+7)>>3)*((64+7)>>3)),
	}
	roi.SegmentID[0] = 1
	roi.DeltaQuantizer[1] = -10
	if err := e.SetROIMap(&roi); err != nil {
		t.Fatalf("SetROIMap: %v", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: 96, Height: 80}); err != nil {
		t.Fatalf("SetRealtimeTarget resize: %v", err)
	}
	if e.activeMapEnabled || e.activeMapMiRows != 0 || e.activeMapMiCols != 0 {
		t.Fatalf("active map after resize = enabled:%t mi:%dx%d, want disabled",
			e.activeMapEnabled, e.activeMapMiRows, e.activeMapMiCols)
	}
	if e.roi.enabled || e.roi.rows != 0 || e.roi.cols != 0 {
		t.Fatalf("ROI map after resize = enabled:%t dims:%dx%d, want disabled",
			e.roi.enabled, e.roi.rows, e.roi.cols)
	}
}
