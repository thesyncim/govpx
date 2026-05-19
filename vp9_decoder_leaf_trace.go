package govpx

type vp9DecodedLeafTrace struct {
	KeyFrame      bool
	IntraOnly     bool
	MIRow         int
	MICol         int
	BSize         int
	Mode          int
	UvMode        int
	Ref0          int
	Ref1          int
	Mv0Row        int
	Mv0Col        int
	Mv1Row        int
	Mv1Col        int
	InterpFilter  int
	TxSize        int
	Skip          int
	SegmentID     int
	TxBlockCount  int
	TokenCount    int
	EOBTotal      int
	QCoeffNonZero int
	QCoeffAbsSum  int
}
