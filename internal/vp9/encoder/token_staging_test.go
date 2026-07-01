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

	buf.Release()
	if buf.Tokens != nil {
		t.Fatal("Tokens retained after Release")
	}
	if buf.Lists != nil {
		t.Fatal("Lists retained after Release")
	}
	if buf.Used != 0 || buf.miRows != 0 || buf.miCols != 0 {
		t.Fatalf("Release state = used:%d rows:%d cols:%d, want zeros",
			buf.Used, buf.miRows, buf.miCols)
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
