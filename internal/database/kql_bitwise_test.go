package database

import (
	"math"
	"testing"
)

func TestBitwiseBoundaries(t *testing.T) {
	for _, value := range []int64{0, 1, -1, math.MinInt64, math.MaxInt64} {
		if kqlBinaryAnd(value, int64(-1)) != value || kqlBinaryOr(value, int64(0)) != value || kqlBinaryXor(value, value) != int64(0) || kqlBinaryNot(kqlBinaryNot(value)) != value {
			t.Fatal("integer identities failed")
		}
		for count := int64(0); count < 64; count++ {
			if kqlBinaryShiftLeft(value, count) != kqlBinaryShiftLeft(value, count+64) || kqlBinaryShiftRight(value, count) != kqlBinaryShiftRight(value, count+128) {
				t.Fatal("shift modulus failed")
			}
		}
	}
	for _, value := range []any{nil, []byte(nil), float64(1), "1"} {
		if kqlBinaryAnd(value, int64(1)) != nil || kqlBinaryOr(int64(1), value) != nil || kqlBinaryXor(value, int64(1)) != nil || kqlBinaryNot(value) != nil || kqlBitsetCountOnes(value) != nil || kqlBinaryShiftLeft(int64(1), value) != nil || kqlBinaryShiftRight(value, int64(1)) != nil {
			t.Fatal("invalid runtime input accepted")
		}
	}
	for _, shift := range []int64{-1, math.MinInt64} {
		if kqlBinaryShiftLeft(int64(1), shift) != nil || kqlBinaryShiftRight(int64(1), shift) != nil {
			t.Fatal("negative shift accepted")
		}
	}
	if kqlBinaryShiftLeft(int64(1), int64(math.MaxInt64)) != int64(math.MinInt64) || kqlBitsetCountOnes(int64(math.MinInt64)) != int64(1) {
		t.Fatal("high bit or shift-count boundary failed")
	}
}

func FuzzBitwiseIdentities(f *testing.F) {
	f.Add(int64(-1), int64(42))
	f.Add(int64(math.MinInt64), int64(math.MaxInt64))
	f.Fuzz(func(t *testing.T, a, b int64) {
		notA := kqlBinaryNot(a).(int64)
		notB := kqlBinaryNot(b).(int64)
		if kqlBinaryNot(kqlBinaryAnd(a, b)) != kqlBinaryOr(notA, notB) || kqlBinaryNot(kqlBinaryOr(a, b)) != kqlBinaryAnd(notA, notB) {
			t.Fatal("De Morgan identity failed")
		}
		if kqlBitsetCountOnes(a).(int64)+kqlBitsetCountOnes(notA).(int64) != 64 {
			t.Fatal("population count complement failed")
		}
		shift := int64(uint64(b) % 64)
		left := kqlBinaryShiftLeft(a, shift).(int64)
		for bit := int64(0); bit < 64; bit++ {
			want := int64(0)
			if bit >= shift {
				want = (a >> uint(bit-shift)) & 1
			}
			if (left>>uint(bit))&1 != want {
				t.Fatal("shift bit mapping failed")
			}
		}
	})
}
