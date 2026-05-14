//go:build !govpx_oracle_trace

package govpx

const vp9OracleTraceBuild = false

type vp9OracleFrameSummary struct {
	Row                  string
	FrameIndex           int
	Flags                uint32
	Dropped              bool
	DropReason           string
	KeyFrame             bool
	IntraOnly            bool
	ShowFrame            bool
	Droppable            bool
	BaseQIndex           int
	PublicQuantizer      int
	SizeBytes            int
	FirstPartitionSize   int
	RefreshFrameFlags    uint8
	RefreshFrameContext  bool
	ErrorResilient       bool
	FrameParallel        bool
	FrameContextIdx      uint8
	TxMode               int
	InterpFilter         int
	ReferenceMode        int
	CompoundAllowed      bool
	ReferenceMask        uint8
	LoopFilterLevel      int
	TemporalLayerID      int
	TemporalLayerCount   int
	TemporalLayerSync    bool
	TL0PICIDX            uint8
	TargetBitrateKbps    int
	FrameTargetBits      int
	BufferLevelBits      int
	ActiveBestQ          int
	ActiveWorstQ         int
	RateCorrectionFactor float64
	RecodeAllowed        bool
	RecodeLoopCount      int
	TileLog2Cols         int
	TileLog2Rows         int
}

func (e *VP9Encoder) resetVP9OracleTraceState() {}

func (e *VP9Encoder) resetVP9OracleRateSelectionTrace() {}

func (e *VP9Encoder) recordVP9OracleRateSelectionTrace(int, int, float64, bool, int) {
}

func (e *VP9Encoder) vp9OracleRateSelectionTrace() (int, int, float64, bool, int) {
	return 0, 0, 0, false, 0
}

func (e *VP9Encoder) emitVP9OracleFrameTrace(vp9OracleFrameSummary) {}
