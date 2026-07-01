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
	buf.Lists[3] = TokenList{Start: 1, Stop: 6, Count: 5}

	buf.Reset()
	if buf.Used != 0 {
		t.Fatalf("Used after Reset = %d, want 0", buf.Used)
	}
	if got := buf.Lists[3]; got != (TokenList{}) {
		t.Fatalf("Reset left stale TokenList = %+v", got)
	}
	buf.Ensure(0, 0)
	if len(buf.Tokens) != 0 || len(buf.Lists) != 0 {
		t.Fatalf("Ensure(0,0) lens = tokens:%d lists:%d, want zeros",
			len(buf.Tokens), len(buf.Lists))
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
	if buf.Used != 0 || buf.miRows != 0 || buf.miCols != 0 || buf.sbRows != 0 {
		t.Fatalf("Release state = used:%d rows:%d cols:%d sbRows:%d, want zeros",
			buf.Used, buf.miRows, buf.miCols, buf.sbRows)
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
