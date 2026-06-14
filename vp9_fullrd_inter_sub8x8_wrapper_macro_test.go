package govpx

import "testing"

func TestVP9Sub8x8BestYRDFromLibvpxMacroPrecedence(t *testing.T) {
	got := vp9Sub8x8BestYRDFromLibvpxMacro(150296, 7,
		29050026, 164, 30736)
	const want = 32936092
	if got != want {
		t.Fatalf("best_yrd macro-precedence result = %d, want %d", got, want)
	}
}
