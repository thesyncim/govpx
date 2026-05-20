package arith

import "testing"

func TestSaturatingAdd(t *testing.T) {
	maxInt := MaxInt()
	tests := []struct {
		name string
		a    int
		b    int
		want int
	}{
		{name: "normal", a: 11, b: 7, want: 18},
		{name: "positive overflow", a: maxInt - 4, b: 8, want: maxInt},
		{name: "negative overflow", a: -maxInt + 4, b: -8, want: -maxInt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SaturatingAdd(tt.a, tt.b); got != tt.want {
				t.Fatalf("SaturatingAdd(%d, %d) = %d, want %d",
					tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestSaturatingSub(t *testing.T) {
	maxInt := MaxInt()
	tests := []struct {
		name string
		a    int
		b    int
		want int
	}{
		{name: "normal", a: 11, b: 7, want: 4},
		{name: "positive overflow", a: maxInt - 4, b: -8, want: maxInt},
		{name: "negative overflow", a: -maxInt + 4, b: 8, want: -maxInt},
		{name: "minimum negation", a: 3, b: -maxInt, want: maxInt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SaturatingSub(tt.a, tt.b); got != tt.want {
				t.Fatalf("SaturatingSub(%d, %d) = %d, want %d",
					tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestPercentOf(t *testing.T) {
	maxInt := MaxInt()
	tests := []struct {
		value int
		pct   int
		want  int
	}{
		{value: 200, pct: 25, want: 50},
		{value: -200, pct: 25, want: 0},
		{value: 200, pct: 0, want: 0},
		{value: maxInt, pct: 2, want: maxInt},
	}
	for _, tt := range tests {
		if got := PercentOf(tt.value, tt.pct); got != tt.want {
			t.Fatalf("PercentOf(%d, %d) = %d, want %d",
				tt.value, tt.pct, got, tt.want)
		}
	}
}

func TestCheckedMul(t *testing.T) {
	maxInt := MaxInt()
	tests := []struct {
		a    int
		b    int
		want int
		ok   bool
	}{
		{a: 9, b: 7, want: 63, ok: true},
		{a: 0, b: maxInt, want: 0, ok: true},
		{a: -1, b: 7, want: 0, ok: false},
		{a: maxInt, b: 2, want: 0, ok: false},
	}
	for _, tt := range tests {
		got, ok := CheckedMul(tt.a, tt.b)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("CheckedMul(%d, %d) = %d/%t, want %d/%t",
				tt.a, tt.b, got, ok, tt.want, tt.ok)
		}
	}
}
