//go:build goexperiment.simd && (amd64 || arm64) && !purego

package pixel

import (
	"bytes"
	"fmt"
	"simd/archsimd"
	"slices"
	"testing"
)

// fpPacketScalar derives the public integer contract independently. It includes
// parameters outside libvpx's encoder-table range, which must retain fallback.
func fpPacketScalar(c, scan, qo, dq []int16, n int, r, q, d [2]int16, cl []byte) (eob int) {
	for i := range n {
		k := 1
		if i == 0 {
			k = 0
		}
		m := int64(c[i])
		if m < 0 {
			m = -m
		}
		m += int64(r[k])
		v := int64(0)
		if m >= int64(d[k]) {
			if m < -32768 {
				m = -32768
			}
			if m > 32767 {
				m = 32767
			}
			v = m * int64(q[k]) >> 16
			if c[i] < 0 {
				v = -v
			}
		}
		if qo != nil {
			qo[i] = int16(v)
		}
		dq[i] = int16(v * int64(d[k]))
		if v != 0 && int(scan[i]) > eob {
			eob = int(scan[i])
		}
		if cl != nil {
			if v < 0 {
				v = -v
			}
			var value byte
			for _, threshold := range [...]int64{0, 1, 2, 4, 10} {
				if v > threshold {
					value++
				}
			}
			cl[i] = value
		}
	}
	return
}

// fpPacketFrozen8 freezes the pre-change public checks and packet store order.
// It is used only when aliasing makes a scalar walk a different operation.
func fpPacketFrozen8(coeff, iscan, qcoeff, dqcoeff []int16, n int, r, q, d [2]int16, classes []byte) int {
	if n <= 0 {
		return 0
	}
	_ = coeff[n-1]
	_ = iscan[n-1]
	_ = dqcoeff[n-1]
	if qcoeff != nil {
		_ = qcoeff[n-1]
	}
	if classes != nil {
		_ = classes[n-1]
	}
	if !Enabled || r[0] < 0 || r[1] < 0 || q[0] < 0 || q[1] < 0 || d[0] <= 0 || d[1] <= 0 {
		return fpPacketScalar(coeff, iscan, qcoeff, dqcoeff, n, r, q, d, classes)
	}
	acR, acQ, acD := archsimd.BroadcastInt16x8(r[1]), archsimd.BroadcastInt16x8(q[1]), archsimd.BroadcastInt16x8(d[1])
	dcR, dcQ, dcD := acR.SetElem(0, r[0]), acQ.SetElem(0, q[0]), acD.SetElem(0, d[0])
	max16 := archsimd.BroadcastUint16x8(32767)
	zero, last := archsimd.Int16x8{}, archsimd.Int16x8{}
	lut := archsimd.LoadUint8x16Array(&fpClassLUT)
	i := 0
	for ; i+8 <= n; i += 8 {
		vr, vq, vd := acR, acQ, acD
		if i == 0 {
			vr, vq, vd = dcR, dcQ, dcD
		}
		c := archsimd.LoadInt16x8(coeff[i:])
		a := c.Abs().ToBits().Add(vr.ToBits()).Min(max16).BitsToInt16()
		t := fpMulHigh16(a, vq).Masked(a.GreaterEqual(vd))
		magnitude := t
		t = zero.Sub(t).IfElse(c.Less(zero), t)
		if qcoeff != nil {
			t.Store(qcoeff[i:])
		}
		t.Mul(vd).Store(dqcoeff[i:])
		last = last.Max(archsimd.LoadInt16x8(iscan[i:]).Masked(t.NotEqual(zero)))
		if classes != nil {
			indices := fpClassIndices(magnitude.Min(archsimd.BroadcastInt16x8(11)))
			storeBytes8(classes[i:], permute16(lut, indices))
		}
	}
	// Keep the global DC/AC index for any scalar remainder.
	eob := fpReduceMax16(last)
	for ; i < n; i++ {
		var qc []int16
		if qcoeff != nil {
			qc = qcoeff[i : i+1]
		}
		var cl []byte
		if classes != nil {
			cl = classes[i : i+1]
		}
		k := 1
		if i == 0 {
			k = 0
		}
		v := fpPacketScalar(coeff[i:i+1], iscan[i:i+1], qc, dqcoeff[i:i+1], 1,
			[2]int16{r[k], r[k]}, [2]int16{q[k], q[k]}, [2]int16{d[k], d[k]}, cl)
		if v > eob {
			eob = v
		}
	}
	return eob
}

func fpPacketRun(t *testing.T, n, options, alignment, pattern, lane int, r, q, d [2]int16) {
	t.Helper()
	var got16, want16 [4][]int16
	var offsets16 [4]int
	for k := range got16 {
		offsets16[k] = 33 + (alignment*(2*k+1)+k)%32
		got16[k] = make([]int16, offsets16[k]+n+19)
		for i := range got16[k] {
			got16[k][i] = int16(0x5757)
		}
	}
	off8 := 65 + (alignment*9+4)%64
	got8 := bytes.Repeat([]byte{0xa7}, off8+n+19)
	c, scan := got16[0][offsets16[0]:offsets16[0]+n], got16[1][offsets16[1]:offsets16[1]+n]
	for i := range c {
		k := 1
		if i == 0 {
			k = 0
		}
		below := max(0, int(d[k])-int(r[k])-1)
		at := max(0, int(d[k])-int(r[k]))
		v := 0
		switch pattern {
		case 1:
			v = min(below, 32767)
		case 2:
			v = min(at, 32767)
		case 3:
			if i == 0 {
				v = 32767
			}
		case 4:
			if i == n-1 {
				v = -32768
			}
		case 5:
			if (i/32)%2 != 0 {
				v = (i*347)%65536 - 32768
			}
		case 6:
			v = [...]int{-32768, -32767, -1, 0, 1, 32766, 32767}[i%7]
		case 7:
			if i == n-32+lane {
				v = 32767
			}
		case 8:
			if i == n-32+lane {
				v = -32768
			}
		}
		c[i] = int16(v)
		scan[i] = int16((i * 37) % (n + 1))
	}
	for k := range got16 {
		want16[k] = slices.Clone(got16[k])
	}
	want8 := bytes.Clone(got8)
	qg, dg := got16[2][offsets16[2]:offsets16[2]+n], got16[3][offsets16[3]:offsets16[3]+n]
	qw, dw := want16[2][offsets16[2]:offsets16[2]+n], want16[3][offsets16[3]:offsets16[3]+n]
	cg, cw := got8[off8:off8+n:off8+n], want8[off8:off8+n:off8+n]
	if options&1 != 0 {
		qg, qw = nil, nil
	}
	if options&2 != 0 {
		cg, cw = nil, nil
	}
	we := fpPacketScalar(want16[0][offsets16[0]:offsets16[0]+n], want16[1][offsets16[1]:offsets16[1]+n], qw, dw, n, r, q, d, cw)
	ge := QuantizeFPBlock(c, scan, qg, dg, n, r, q, d, cg)
	if ge != we {
		t.Fatalf("n%d options%d alignment%d pattern%d lane%d EOB%d/%d", n, options, alignment, pattern, lane, ge, we)
	}
	for k := range got16 {
		if !slices.Equal(got16[k], want16[k]) {
			t.Fatalf("n%d options%d alignment%d pattern%d lane%d backing%d", n, options, alignment, pattern, lane, k)
		}
	}
	if !bytes.Equal(got8, want8) {
		t.Fatalf("n%d options%d alignment%d pattern%d lane%d backing4", n, options, alignment, pattern, lane)
	}
}

func TestFPPacketIndependent(t *testing.T) {
	tables := [][3][2]int16{
		{{0, 17}, {32767, 12345}, {23, 41}},
		{{32767, 32767}, {32767, 32767}, {1, 32767}},
		{{0, 17}, {0, 0}, {23, 41}},
		{{0, 0}, {32767, 1}, {32767, 1}},
		{{-1, 17}, {32767, 12345}, {23, 41}},
		{{0, 17}, {-32768, 12345}, {-32768, 0}},
	}
	count := 0
	for _, n := range []int{7, 8, 9, 16, 17, 63, 64, 65, 80, 224, 256, 257, 288, 1024} {
		for options := range 4 {
			for _, alignment := range []int{0, 1, 63} {
				for _, p := range tables {
					for pattern := range 7 {
						fpPacketRun(t, n, options, alignment, pattern, 0, p[0], p[1], p[2])
						count++
					}
				}
			}
		}
	}
	for _, n := range []int{64, 256} {
		for options := range 4 {
			for lane := range 32 {
				for _, pattern := range []int{7, 8} {
					p := tables[0]
					fpPacketRun(t, n, options, 1, pattern, lane, p[0], p[1], p[2])
					count++
				}
			}
		}
	}
	t.Logf("independent full-backing numeric cases=%d", count)
}

func fpPacketCapture(fn func() int) (eob int, panicText string) {
	defer func() {
		if p := recover(); p != nil {
			panicText = fmt.Sprint(p)
		}
	}()
	return fn(), ""
}

func TestFPPacketAliasOrder(t *testing.T) {
	count := 0
	for _, n := range []int{16, 64, 256} {
		for a := range 4 {
			for b := a + 1; b < 4; b++ {
				for _, shift := range []int{0, 1, -1, n - 1, -n + 1} {
					for pattern := range 3 {
						off := [4]int{512, 2048, 3584, 5120}
						off[b] = off[a] + shift
						got16 := make([]int16, 8192)
						for i := range got16 {
							got16[i] = int16(0x5757)
						}
						got8 := bytes.Repeat([]byte{0xa7}, 512)
						c := got16[off[0] : off[0]+n]
						for i := range c {
							c[i] = 0
							if pattern == 1 {
								c[i] = 9
							}
							if pattern == 2 && i == n-1 {
								c[i] = -32768
							}
						}
						want16 := slices.Clone(got16)
						want8 := bytes.Clone(got8)
						run := func(buf16 []int16, buf8 []byte, frozen bool) int {
							c, s, q, d := buf16[off[0]:off[0]+n], buf16[off[1]:off[1]+n], buf16[off[2]:off[2]+n], buf16[off[3]:off[3]+n]
							cl := buf8[64 : 64+n : 64+n]
							r, qt, dt := [2]int16{0, 17}, [2]int16{32767, 12345}, [2]int16{23, 41}
							if frozen {
								return fpPacketFrozen8(c, s, q, d, n, r, qt, dt, cl)
							}
							return QuantizeFPBlock(c, s, q, d, n, r, qt, dt, cl)
						}
						we := run(want16, want8, true)
						ge := run(got16, got8, false)
						if ge != we || !slices.Equal(got16, want16) || !bytes.Equal(got8, want8) {
							t.Fatalf("n%d pair%d/%d shift%d pattern%d EOB%d/%d", n, a, b, shift, pattern, ge, we)
						}
						count++
					}
				}
			}
		}
	}
	t.Logf("frozen alias full-backing cases=%d", count)
}

func TestFPPacketPanicWindows(t *testing.T) {
	count := 0
	for _, n := range []int{16, 64, 256} {
		for short := range 5 {
			for _, length := range []int{0, n - 1} {
				var got, want [5][]int16
				for i := range got {
					got[i] = make([]int16, n)
					want[i] = make([]int16, n)
					if i == 2 || i == 3 {
						for j := range got[i] {
							got[i][j] = 0x4a4a
							want[i][j] = 0x4a4a
						}
					}
				}
				gc, wc := bytes.Repeat([]byte{0xa7}, n), bytes.Repeat([]byte{0xa7}, n)
				if short < 4 {
					got[short] = got[short][:length:length]
					want[short] = want[short][:length:length]
				} else {
					gc = gc[:length:length]
					wc = wc[:length:length]
				}
				r, q, d := [2]int16{0, 17}, [2]int16{32767, 12345}, [2]int16{23, 41}
				ge, gp := fpPacketCapture(func() int { return QuantizeFPBlock(got[0], got[1], got[2], got[3], n, r, q, d, gc) })
				we, wp := fpPacketCapture(func() int { return fpPacketFrozen8(want[0], want[1], want[2], want[3], n, r, q, d, wc) })
				if gp == "" || gp != wp || ge != we || !bytes.Equal(gc, wc) {
					t.Fatalf("n%d short%d length%d panic%q/%q", n, short, length, gp, wp)
				}
				for k := range got {
					for i := range got[k] {
						if got[k][i] != want[k][i] {
							t.Fatal("panic wrote output")
						}
					}
				}
				count++
			}
		}
	}
	t.Logf("frozen pre-write panic cases=%d", count)
}

// A zero dequantized coefficient does not imply a zero quantized coefficient.
// This valid public-table case deliberately wraps 4*16384 to int16(0).
func TestFPPacketDequantWrapZero(t *testing.T) {
	count := 0
	r, q, d := [2]int16{}, [2]int16{16, 16}, [2]int16{16384, 16384}
	for _, n := range []int{16, 64, 256, 288, 1024} {
		for options := range 4 {
			for _, alignment := range []int{0, 1, 63} {
				fpPacketRun(t, n, options, alignment, 2, 0, r, q, d)
				count++
			}
		}
	}
	const n = 256
	c, scan, qo, dq, cl := make([]int16, n), make([]int16, n), make([]int16, n), make([]int16, n), make([]byte, n)
	for i := range c {
		c[i], scan[i] = 16384, int16(i+1)
	}
	if eob := QuantizeFPBlock(c, scan, qo, dq, n, r, q, d, cl); eob != n {
		t.Fatalf("zero dq discarded nonzero q EOB: %d", eob)
	}
	for i := range qo {
		if qo[i] != 4 || dq[i] != 0 || cl[i] != 3 {
			t.Fatalf("wrap lane%d q%d dq%d class%d", i, qo[i], dq[i], cl[i])
		}
	}
	t.Logf("deliberate nonzero-q/zero-dq full-backing cases=%d plus literal EOB/class assertion", count)
}
