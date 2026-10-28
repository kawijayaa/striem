package database

import (
	"encoding/binary"
	"math"
	"net/netip"
	"strings"
	"testing"
)

func TestIPv6ParsingDocumentedExamples(t *testing.T) {
	store, err := Open(t.TempDir() + "/ipv6.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, tc := range []struct {
		ip     string
		prefix int64
		want   string
	}{
		{"192.168.255.255", 120, "0000:0000:0000:0000:0000:ffff:c0a8:ff00"},
		{"192.168.255.255/24", 124, "0000:0000:0000:0000:0000:ffff:c0a8:ff00"},
		{"255.255.255.255", 128, "0000:0000:0000:0000:0000:ffff:ffff:ffff"},
		{"fe80::85d:e82c:9446:7994", 128, "fe80:0000:0000:0000:085d:e82c:9446:7994"},
		{"fe80::85d:e82c:9446:7994/120", 124, "fe80:0000:0000:0000:085d:e82c:9446:7900"},
		{"::192.168.255.255", 128, "0000:0000:0000:0000:0000:ffff:c0a8:ffff"},
		{"::192.168.255.255/24", 128, "0000:0000:0000:0000:0000:ffff:c0a8:ff00"},
		{"ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", 0, "0000:0000:0000:0000:0000:0000:0000:0000"},
		{"ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", 127, "ffff:ffff:ffff:ffff:ffff:ffff:ffff:fffe"},
	} {
		if got := querySQLiteFunction(t, store.DB(), "SELECT kql_parse_ipv6_mask(?,?)", tc.ip, tc.prefix); got != tc.want {
			t.Fatalf("%s/%d got %s want %s", tc.ip, tc.prefix, got, tc.want)
		}
	}
	for _, tc := range []struct{ ip, want string }{
		{"192.168.255.255", "0000:0000:0000:0000:0000:ffff:c0a8:ffff"},
		{"192.168.255.255/24", "0000:0000:0000:0000:0000:ffff:c0a8:ff00"},
		{"255.255.255.255", "0000:0000:0000:0000:0000:ffff:ffff:ffff"},
		{"::", "0000:0000:0000:0000:0000:0000:0000:0000"},
		{"::1", "0000:0000:0000:0000:0000:0000:0000:0001"},
		{"2001:DB8::ABCD", "2001:0db8:0000:0000:0000:0000:0000:abcd"},
	} {
		if got := querySQLiteFunction(t, store.DB(), "SELECT kql_parse_ipv6(?)", tc.ip); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.ip, got, tc.want)
		}
	}
	for _, input := range []any{nil, "", "bad", "localhost", "1.2.3", "1.2.3.4/33", "::1/129", "::1/-1", "::1/+1", "::1/1/2", "fe80::1%eth0", "[::1]", " ::1", "::1\x00", "::1/", strings.Repeat("0", 1024), int64(1)} {
		if got := querySQLiteFunction(t, store.DB(), "SELECT kql_parse_ipv6(?)", input); got != "" {
			t.Fatalf("invalid input accepted: %T", input)
		}
	}
	for _, prefix := range []any{nil, int64(-1), int64(129), int64(math.MaxInt64), float64(64)} {
		if got := querySQLiteFunction(t, store.DB(), "SELECT kql_parse_ipv6_mask('::1',?)", prefix); got != "" {
			t.Fatalf("invalid prefix accepted: %v", prefix)
		}
	}
}

func TestIPv6ComparisonDocumentedExamples(t *testing.T) {
	store, err := Open(t.TempDir() + "/ipv6-compare.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, pair := range [][2]string{
		{"192.168.1.1", "192.168.1.1"},
		{"192.168.1.1/24", "192.168.1.255"},
		{"192.168.1.1", "192.168.1.255/24"},
		{"192.168.1.1/30", "192.168.1.255/24"},
		{"fe80::85d:e82c:9446:7994", "fe80::85d:e82c:9446:7994"},
		{"fe80::85d:e82c:9446:7994/120", "fe80::85d:e82c:9446:7998"},
		{"fe80::85d:e82c:9446:7994", "fe80::85d:e82c:9446:7998/120"},
		{"fe80::85d:e82c:9446:7994/120", "fe80::85d:e82c:9446:7998/120"},
		{"192.168.1.1", "::ffff:c0a8:0101"},
		{"192.168.1.1/24", "::ffff:c0a8:01ff"},
		{"::ffff:c0a8:0101", "192.168.1.255/24"},
		{"::192.168.1.1/30", "192.168.1.255/24"},
	} {
		if got := querySQLiteFunction(t, store.DB(), "SELECT kql_ipv6_compare(?,?)", pair[0], pair[1]); got != int64(0) {
			t.Fatalf("documented equality failed: %v %v", pair, got)
		}
		if got := querySQLiteFunction(t, store.DB(), "SELECT kql_ipv6_is_match(?,?)", pair[0], pair[1]); got != int64(1) {
			t.Fatalf("documented match failed: %v %v", pair, got)
		}
	}
	for _, tc := range []struct {
		a, b   string
		prefix int64
	}{
		{"192.168.1.1", "192.168.1.0", 31},
		{"192.168.1.1/24", "192.168.1.255", 31},
		{"192.168.1.1", "192.168.1.255", 24},
		{"fe80::85d:e82c:9446:7994", "fe80::85d:e82c:9446:7995", 127},
		{"fe80::85d:e82c:9446:7994/127", "fe80::85d:e82c:9446:7998", 120},
		{"fe80::85d:e82c:9446:7994/120", "fe80::85d:e82c:9446:7998", 127},
		{"192.168.1.1/24", "::ffff:c0a8:01ff", 127},
		{"::ffff:c0a8:0101", "192.168.1.255", 120},
		{"::192.168.1.1/30", "192.168.1.255/24", 127},
	} {
		if got := querySQLiteFunction(t, store.DB(), "SELECT kql_ipv6_compare(?,?,?)", tc.a, tc.b, tc.prefix); got != int64(0) {
			t.Fatalf("masked equality failed: %+v %v", tc, got)
		}
	}
	for _, tc := range []struct {
		a, b string
		want any
	}{
		{"::", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", int64(-1)},
		{"ffff::", "::", int64(1)},
		{"2001:db8::2", "2001:db8::10", int64(-1)},
		{"bad", "::1", nil}, {"::1", "bad", nil},
	} {
		if got := querySQLiteFunction(t, store.DB(), "SELECT kql_ipv6_compare(?,?)", tc.a, tc.b); got != tc.want {
			t.Fatalf("comparison: %+v got %v", tc, got)
		}
	}
	for _, query := range []string{"SELECT kql_ipv6_compare(NULL,'::1')", "SELECT kql_ipv6_is_match('::1',NULL)", "SELECT kql_ipv6_compare('::1','::1',NULL)", "SELECT kql_ipv6_is_match('::1','::1',129)"} {
		if got := querySQLiteFunction(t, store.DB(), query); got != nil {
			t.Fatal("invalid comparison did not return null")
		}
	}
}

func TestIPv6AllPrefixWidths(t *testing.T) {
	address := netip.MustParseAddr("fedc:ba98:7654:3210:89ab:cdef:0123:4567")
	for prefix := 0; prefix <= 128; prefix++ {
		text := kqlParseIPv6Mask(address.String(), int64(prefix))
		got, err := netip.ParseAddr(text)
		want := netip.PrefixFrom(address, prefix).Masked().Addr()
		if err != nil || got != want || len(text) != 39 {
			t.Fatalf("/%d got %s want %s error=%v", prefix, text, want, err)
		}
	}
}

func FuzzIPv6ParsingAndComparison(f *testing.F) {
	f.Add("192.168.1.1/24", "::ffff:c0a8:01ff", int64(120), uint64(0x20010db800000000), uint64(1))
	f.Add("fe80::1", "fe80::2", int64(128), uint64(0xffffffffffffffff), uint64(0xffffffffffffffff))
	f.Fuzz(func(t *testing.T, a, b string, prefix int64, high, low uint64) {
		if len(a) > 128 || len(b) > 128 {
			t.Skip()
		}
		parsed := kqlParseIPv6(a)
		if parsed != "" {
			if len(parsed) != 39 || kqlParseIPv6(parsed) != parsed {
				t.Fatal("canonical parse is not idempotent")
			}
		}
		order, reverse, match := kqlIPv6Compare(a, b, prefix), kqlIPv6Compare(b, a, prefix), kqlIPv6IsMatch(a, b, prefix)
		if order == nil {
			if reverse != nil || match != nil {
				t.Fatal("invalid comparison lost symmetry")
			}
		} else {
			value := order.(int64)
			if value < -1 || value > 1 || reverse != -value || match != (value == 0) {
				t.Fatal("comparison and match disagree")
			}
		}
		var data [16]byte
		binary.BigEndian.PutUint64(data[:8], high)
		binary.BigEndian.PutUint64(data[8:], low)
		address := netip.AddrFrom16(data)
		text := kqlParseIPv6Mask(expandedIPv6(data), prefix)
		if prefix < 0 || prefix > 128 {
			if text != "" {
				t.Fatal("invalid prefix accepted")
			}
			return
		}
		got, err := netip.ParseAddr(text)
		if err != nil || got != netip.PrefixFrom(address, int(prefix)).Masked().Addr() {
			t.Fatal("mask differs from 128-bit prefix")
		}
	})
}
