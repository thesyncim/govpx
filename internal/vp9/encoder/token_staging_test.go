package encoder

import "testing"

func TestTokenAllocForMIMatchesLibvpxFormula(t *testing.T) {
	tests := []struct {
		name           string
		miRows, miCols int
		wantTokens     int
		wantLists      int
	}{
		{name: "empty", wantTokens: 0, wantLists: 0},
		{name: "one mi", miRows: 1, miCols: 1, wantTokens: 4 * 4 * (16*16*3 + 4), wantLists: 1 * 4 * 64},
		{name: "one sb", miRows: 8, miCols: 8, wantTokens: 4 * 4 * (16*16*3 + 4), wantLists: 1 * 4 * 64},
		{name: "720p", miRows: 90, miCols: 160, wantTokens: 48 * 80 * (16*16*3 + 4), wantLists: 12 * 4 * 64},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := TokenAllocForMI(tc.miRows, tc.miCols); got != tc.wantTokens {
				t.Fatalf("TokenAllocForMI(%d,%d) = %d, want %d",
					tc.miRows, tc.miCols, got, tc.wantTokens)
			}
			if got := TokenListAllocForMI(tc.miRows); got != tc.wantLists {
				t.Fatalf("TokenListAllocForMI(%d) = %d, want %d",
					tc.miRows, got, tc.wantLists)
			}
		})
	}
}

func TestTokenFrameBufferTileLocalListIndexAndSlices(t *testing.T) {
	var buf TokenFrameBuffer
	buf.EnsureForTile(90, 40, 2, 17)
	if got, want := len(buf.Lists), TokenListAllocForTileGrid(90, 1, 1); got != want {
		t.Fatalf("tile-local Lists len = %d, want %d", got, want)
	}
	if got, want := len(buf.Tokens), TokenAllocForMI(90, 40); got != want {
		t.Fatalf("tile-local Tokens len = %d, want %d", got, want)
	}
	idx, ok := buf.TokenListIndex(2, 17, 11)
	if !ok {
		t.Fatal("TokenListIndex rejected tile-local global coordinate")
	}
	if idx != 11 {
		t.Fatalf("TokenListIndex tile-local idx = %d, want 11", idx)
	}
	if _, ok := buf.TokenListIndex(1, 17, 0); ok {
		t.Fatal("TokenListIndex accepted tile row before tile-local base")
	}
	if _, ok := buf.TokenListIndex(2, 16, 0); ok {
		t.Fatal("TokenListIndex accepted tile col before tile-local base")
	}
	if _, ok := buf.TokenListIndex(2, 18, 0); ok {
		t.Fatal("TokenListIndex accepted tile col after tile-local extent")
	}

	idx, ok = buf.StartTokenList(2, 17, 3)
	if !ok {
		t.Fatal("StartTokenList rejected tile-local global coordinate")
	}
	for _, token := range []int16{OneToken, EOSBToken} {
		if !buf.AppendToken(TokenExtra{Token: token}) {
			t.Fatalf("AppendToken(%d) returned false", token)
		}
	}
	if !buf.AppendLeafMode(9) {
		t.Fatal("AppendLeafMode returned false")
	}
	if !buf.AppendPartition(2) {
		t.Fatal("AppendPartition returned false")
	}
	if !buf.FinishTokenList(idx) {
		t.Fatal("FinishTokenList returned false for tile-local list")
	}
	tokens, ok := buf.TokensForList(buf.Lists[idx])
	if !ok {
		t.Fatal("TokensForList rejected tile-local list")
	}
	if len(tokens) != 2 || tokens[1].Token != EOSBToken {
		t.Fatalf("tile-local tokens = %+v, want one token plus EOSB", tokens)
	}
	modes, ok := buf.LeafModesForList(buf.LeafLists[idx])
	if !ok || len(modes) != 1 || modes[0] != 9 {
		t.Fatalf("tile-local leaf modes = %v, want [9]", modes)
	}
	partitions, ok := buf.PartitionsForList(buf.PartitionLists[idx])
	if !ok || len(partitions) != 1 || partitions[0] != 2 {
		t.Fatalf("tile-local partitions = %v, want [2]", partitions)
	}
}

func TestTokenFrameBufferEnsureResetAndRelease(t *testing.T) {
	var buf TokenFrameBuffer
	buf.Ensure(8, 8)
	if got, want := len(buf.Tokens), TokenAllocForMI(8, 8); got != want {
		t.Fatalf("Tokens len = %d, want %d", got, want)
	}
	if got, want := len(buf.Lists), TokenListAllocForMI(8); got != want {
		t.Fatalf("Lists len = %d, want %d", got, want)
	}
	if got := buf.sbRows; got != 1 {
		t.Fatalf("sbRows = %d, want 1", got)
	}
	buf.Used = 7
	buf.LeafUsed = 2
	buf.PartitionUsed = 3
	buf.Lists[3] = TokenList{Start: 1, Stop: 6, Count: 5}
	buf.LeafLists[3] = TokenList{Start: 1, Stop: 2, Count: 1}
	buf.PartitionLists[3] = TokenList{Start: 1, Stop: 3, Count: 2}

	buf.Reset()
	if buf.Used != 0 {
		t.Fatalf("Used after Reset = %d, want 0", buf.Used)
	}
	if buf.LeafUsed != 0 {
		t.Fatalf("LeafUsed after Reset = %d, want 0", buf.LeafUsed)
	}
	if buf.PartitionUsed != 0 {
		t.Fatalf("PartitionUsed after Reset = %d, want 0", buf.PartitionUsed)
	}
	if got := buf.Lists[3]; got != (TokenList{}) {
		t.Fatalf("Reset left stale TokenList = %+v", got)
	}
	if got := buf.LeafLists[3]; got != (TokenList{}) {
		t.Fatalf("Reset left stale leaf TokenList = %+v", got)
	}
	if got := buf.PartitionLists[3]; got != (TokenList{}) {
		t.Fatalf("Reset left stale partition TokenList = %+v", got)
	}
	buf.Ensure(0, 0)
	if len(buf.Tokens) != 0 || len(buf.Lists) != 0 ||
		len(buf.LeafModes) != 0 || len(buf.LeafLists) != 0 ||
		len(buf.Partitions) != 0 || len(buf.PartitionLists) != 0 {
		t.Fatalf("Ensure(0,0) retained token or leaf streams")
	}
	if buf.sbRows != 0 {
		t.Fatalf("Ensure(0,0) sbRows = %d, want 0", buf.sbRows)
	}

	buf.Release()
	if buf.Tokens != nil {
		t.Fatal("Tokens retained after Release")
	}
	if buf.Lists != nil {
		t.Fatal("Lists retained after Release")
	}
	if buf.LeafModes != nil || buf.LeafLists != nil {
		t.Fatal("leaf streams retained after Release")
	}
	if buf.Partitions != nil || buf.PartitionLists != nil {
		t.Fatal("partition streams retained after Release")
	}
	if buf.Used != 0 || buf.LeafUsed != 0 || buf.PartitionUsed != 0 ||
		buf.miRows != 0 || buf.miCols != 0 || buf.sbRows != 0 {
		t.Fatalf("Release state = used:%d leafUsed:%d partitionUsed:%d rows:%d cols:%d sbRows:%d, want zeros",
			buf.Used, buf.LeafUsed, buf.PartitionUsed, buf.miRows, buf.miCols, buf.sbRows)
	}
}

func TestTokenFrameBufferListIndexAndSlices(t *testing.T) {
	var buf TokenFrameBuffer
	buf.Ensure(90, 160)

	tests := []struct {
		tileRow, tileCol, tileSBRow int
		want                        int
	}{
		{0, 0, 0, 0},
		{0, 1, 0, 12},
		{1, 0, 0, 64 * 12},
		{3, 63, 11, TokenListAllocForMI(90) - 1},
	}
	for _, tc := range tests {
		got, ok := buf.TokenListIndex(tc.tileRow, tc.tileCol, tc.tileSBRow)
		if !ok {
			t.Fatalf("TokenListIndex(%d,%d,%d) returned !ok",
				tc.tileRow, tc.tileCol, tc.tileSBRow)
		}
		if got != tc.want {
			t.Fatalf("TokenListIndex(%d,%d,%d) = %d, want %d",
				tc.tileRow, tc.tileCol, tc.tileSBRow, got, tc.want)
		}
	}
	if _, ok := buf.TokenListIndex(4, 0, 0); ok {
		t.Fatal("TokenListIndex accepted tileRow beyond MAX_TILE_ROWS")
	}
	if _, ok := buf.TokenListIndex(0, 64, 0); ok {
		t.Fatal("TokenListIndex accepted tileCol beyond MAX_TILE_COLS")
	}
	if _, ok := buf.TokenListIndex(0, 0, 12); ok {
		t.Fatal("TokenListIndex accepted tileSBRow beyond sbRows")
	}

	idx, ok := buf.StartTokenList(0, 1, 2)
	if !ok {
		t.Fatal("StartTokenList returned !ok")
	}
	for _, token := range []int16{OneToken, EobToken, EOSBToken} {
		if !buf.AppendToken(TokenExtra{Token: token}) {
			t.Fatalf("AppendToken(%d) returned false", token)
		}
	}
	if !buf.AppendLeafMode(3) {
		t.Fatal("AppendLeafMode returned false")
	}
	for _, partition := range []uint8{3, 0} {
		if !buf.AppendPartition(partition) {
			t.Fatalf("AppendPartition(%d) returned false", partition)
		}
	}
	if !buf.FinishTokenList(idx) {
		t.Fatal("FinishTokenList returned false")
	}
	list := buf.Lists[idx]
	if list.Start != 0 || list.Stop != 3 || list.Count != 3 {
		t.Fatalf("TokenList = %+v, want start=0 stop=3 count=3", list)
	}
	tokens, ok := buf.TokensForList(list)
	if !ok {
		t.Fatal("TokensForList returned !ok")
	}
	if len(tokens) != 3 || tokens[2].Token != EOSBToken {
		t.Fatalf("TokensForList = %+v, want EOSB-terminated 3-token slice", tokens)
	}
	modes, ok := buf.LeafModesForList(buf.LeafLists[idx])
	if !ok || len(modes) != 1 || modes[0] != 3 {
		t.Fatalf("LeafModesForList = %v, want [3]", modes)
	}
	partitions, ok := buf.PartitionsForList(buf.PartitionLists[idx])
	if !ok || len(partitions) != 2 || partitions[0] != 3 || partitions[1] != 0 {
		t.Fatalf("PartitionsForList = %v, want [3 0]", partitions)
	}
	if _, ok := buf.TokensForList(TokenList{Start: 0, Stop: buf.Used + 1}); ok {
		t.Fatal("TokensForList accepted stop beyond Used")
	}
}

func TestTokenFrameBufferEnsureSteadyStateAlloc(t *testing.T) {
	var buf TokenFrameBuffer
	buf.Ensure(16, 16)

	allocs := testing.AllocsPerRun(25, func() {
		buf.Ensure(16, 16)
	})
	if allocs != 0 {
		t.Fatalf("TokenFrameBuffer.Ensure steady-state allocs = %v, want 0", allocs)
	}
}
