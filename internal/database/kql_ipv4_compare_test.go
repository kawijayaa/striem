package database

import (
	"fmt"
	"math"
	"testing"
)

func TestIPv4ComparisonDocumentedExamples(t *testing.T) {
	store, err := Open(t.TempDir() + "/ipv4-compare.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, tc := range []struct {
		a, b   string
		prefix []any
		want   any
	}{
		{"192.168.1.0", "192.168.1.0", nil, int64(0)},
		{"192.168.1.1/24", "192.168.1.255", nil, int64(0)},
		{"192.168.1.1", "192.168.1.255/24", nil, int64(0)},
		{"192.168.1.1/30", "192.168.1.255/24", nil, int64(0)},
		{"192.168.1.1", "192.168.1.0", []any{int64(31)}, int64(0)},
		{"192.168.1.1/24", "192.168.1.255", []any{int64(31)}, int64(0)},
		{"192.168.1.1", "192.168.1.255", []any{int64(24)}, int64(0)},
		{"0.0.0.0", "255.255.255.255", nil, int64(-1)},
		{"255.255.255.255", "0.0.0.0", nil, int64(1)},
		{"10.0.0.1", "2.0.0.1", nil, int64(1)},
		{"127.255.255.255", "128.0.0.0", nil, int64(-1)},
		{"255.255.255.255/0", "0.0.0.0", nil, int64(0)},
		{"255.255.255.255", "0.0.0.0", []any{int64(0)}, int64(0)},
		{"1.2.3.4", "1.2.3.5", []any{int64(32)}, int64(-1)},
		{"bad", "1.2.3.4", nil, nil},
		{"1.2.3.4", "bad", nil, nil},
		{"::ffff:1.2.3.4", "1.2.3.4", nil, nil},
		{"1.2.3.4", "1.2.3.4", []any{int64(-1)}, nil},
		{"1.2.3.4", "1.2.3.4", []any{int64(33)}, nil},
		{"1.2.3.4", "1.2.3.4", []any{int64(math.MaxInt64)}, nil},
		{"1.2.3.4", "1.2.3.4", []any{nil}, nil},
	} {
		args := append([]any{tc.a, tc.b}, tc.prefix...)
		placeholders := "?,?"
		if len(tc.prefix) == 1 {
			placeholders += ",?"
		}
		got := querySQLiteFunction(t, store.DB(), "SELECT kql_ipv4_compare("+placeholders+")", args...)
		if got != tc.want {
			t.Fatalf("%v: got %v want %v", args, got, tc.want)
		}
		var match any
		if tc.want != nil {
			match = int64(0)
			if tc.want == int64(0) {
				match = int64(1)
			}
		}
		if got := querySQLiteFunction(t, store.DB(), "SELECT kql_ipv4_is_match("+placeholders+")", args...); got != match {
			t.Fatalf("match %v: got %v want %v", args, got, match)
		}
	}
	for prefix := 0; prefix <= 32; prefix++ {
		for _, pair := range [][2]uint32{{0, 0xffffffff}, {0x7fffffff, 0x80000000}, {0x0a000001, 0x02000001}, {0xc0a80101, 0xc0a801fe}} {
			a, b := kqlFormatIPv4(int64(pair[0])), kqlFormatIPv4(int64(pair[1]))
			x, y := uint64(pair[0])>>uint(32-prefix), uint64(pair[1])>>uint(32-prefix)
			want := int64(0)
			if x < y {
				want = -1
			} else if x > y {
				want = 1
			}
			if got := kqlIPv4Compare(a, b, int64(prefix)); got != want {
				t.Fatalf("%s/%d vs %s: %v want %d", a, prefix, b, got, want)
			}
		}
	}
	for _, args := range [][2]any{{nil, "1.2.3.4"}, {"1.2.3.4", nil}, {int64(1), "1.2.3.4"}} {
		if got := querySQLiteFunction(t, store.DB(), "SELECT kql_ipv4_compare(?,?)", args[0], args[1]); got != nil {
			t.Fatal(fmt.Sprint(got))
		}
	}
}

func FuzzIPv4Comparison(f *testing.F) {
	f.Add("192.168.1.1/24", "192.168.1.255", int64(32))
	f.Add("0.0.0.0", "255.255.255.255", int64(0))
	f.Add("127.255.255.255", "128.0.0.0", int64(32))
	f.Fuzz(func(t *testing.T, a, b string, prefix int64) {
		if len(a) > 128 || len(b) > 128 {
			t.Skip()
		}
		order := kqlIPv4Compare(a, b, prefix)
		reverse := kqlIPv4Compare(b, a, prefix)
		match := kqlIPv4IsMatch(a, b, prefix)
		if order == nil {
			if reverse != nil || match != nil {
				t.Fatal("failure lost symmetry")
			}
			return
		}
		value := order.(int64)
		if value < -1 || value > 1 || reverse != -value || match != (value == 0) {
			t.Fatal("comparison and matching disagree")
		}
		if kqlIPv4Compare(a, a, prefix) != int64(0) {
			t.Fatal("valid address not reflexive")
		}
	})
}
