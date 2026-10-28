package database

import (
	"fmt"
	"math"
	"net"
	"strings"
	"testing"
)

func TestIPv4ConversionDocumentedExamplesAndBounds(t *testing.T) {
	store, err := Open(t.TempDir() + "/ipv4.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, tc := range []struct {
		input any
		want  any
	}{
		{"192.168.1.1", int64(3232235777)},
		{"192.168.1.1/24", int64(3232235776)},
		{"255.255.255.255/31", int64(4294967294)},
		{"0.0.0.0", int64(0)},
		{"255.255.255.255", int64(4294967295)},
		{"255.255.255.255/0", int64(0)},
		{nil, nil}, {"", nil}, {"999.1.1.1", nil}, {"1.2.3", nil},
		{"::ffff:192.168.1.1", nil}, {"::1", nil}, {"host.example", nil},
		{" 1.2.3.4", nil}, {"1.2.3.4\x00", nil}, {"1.2.3.4/33", nil},
		{int64(1), nil},
		{strings.Repeat("9", 1<<20), nil},
	} {
		if got := querySQLiteFunction(t, store.DB(), "SELECT kql_parse_ipv4(?)", tc.input); got != tc.want {
			t.Fatalf("parse %v: got %v want %v", tc.input, got, tc.want)
		}
	}
	for _, tc := range []struct {
		input  string
		prefix int64
		want   any
	}{
		{"127.0.0.1", 24, int64(2130706432)},
		{"192.168.1.255", 0, int64(0)},
		{"192.168.1.255", 32, int64(3232236031)},
		{"192.168.1.255/24", 32, int64(3232235776)},
		{"192.168.1.255/24", 16, int64(3232235520)},
		{"1.2.3.4", -1, nil}, {"1.2.3.4", 33, nil}, {"1.2.3.4", math.MaxInt64, nil},
	} {
		if got := querySQLiteFunction(t, store.DB(), "SELECT kql_parse_ipv4_mask(?,?)", tc.input, tc.prefix); got != tc.want {
			t.Fatalf("parse mask %v/%d: got %v want %v", tc.input, tc.prefix, got, tc.want)
		}
	}
	for _, tc := range []struct {
		ip   string
		want any
	}{{"10.1.2.3", int64(32)}, {"192.168.1.1/24", int64(24)}, {"127.0.0.1/16", int64(16)}, {"0.0.0.0/0", int64(0)}, {"bad", nil}} {
		if got := querySQLiteFunction(t, store.DB(), "SELECT kql_ipv4_netmask_suffix(?)", tc.ip); got != tc.want {
			t.Fatalf("suffix %s: %v want %v", tc.ip, got, tc.want)
		}
	}
}

func FuzzIPv4Conversions(f *testing.F) {
	f.Add("192.168.1.1/24", int64(24), int64(3232235777))
	f.Add("0.0.0.0/0", int64(0), int64(0))
	f.Add("255.255.255.255", int64(32), int64(4294967295))
	f.Add("::ffff:1.2.3.4", int64(33), int64(-1))
	f.Fuzz(func(t *testing.T, text string, prefix, number int64) {
		if len(text) > 128 {
			t.Skip()
		}
		parsed := kqlParseIPv4(text)
		if parsed != nil {
			value := parsed.(int64)
			if value < 0 || value > math.MaxUint32 {
				t.Fatal("parsed value outside IPv4 range")
			}
			formatted := kqlFormatIPv4(value)
			if kqlParseIPv4(formatted) != value {
				t.Fatal("parsed address failed to round-trip")
			}
		}
		_ = kqlParseIPv4Mask(text, prefix)
		_ = kqlFormatIPv4Mask(text, prefix)
		_ = kqlIPv4NetmaskSuffix(text)
		formatted := kqlFormatIPv4(number, prefix)
		if number < 0 || number > math.MaxUint32 || prefix < 0 || prefix > 32 {
			if formatted != "" {
				t.Fatal("invalid numeric address/prefix accepted")
			}
			return
		}
		address := net.IPv4(byte(number>>24), byte(number>>16), byte(number>>8), byte(number))
		want := address.Mask(net.CIDRMask(int(prefix), 32)).String()
		if formatted != want {
			t.Fatalf("got %s want %s", formatted, want)
		}
	})
}

func TestIPv4FormatDocumentedExamplesAndBounds(t *testing.T) {
	store, err := Open(t.TempDir() + "/ipv4.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, tc := range []struct {
		input      any
		prefix     any
		want, mask string
	}{
		{"192.168.1.1", int64(24), "192.168.1.0", "192.168.1.0/24"},
		{"192.168.1.1", int64(32), "192.168.1.1", "192.168.1.1/32"},
		{"192.168.1.1/24", int64(32), "192.168.1.0", "192.168.1.0/24"},
		{"192.168.1.1/24", int64(-1), "", ""},
		{int64(3232235777), int64(24), "192.168.1.0", "192.168.1.0/24"},
		{int64(4294967295), int64(32), "255.255.255.255", "255.255.255.255/32"},
		{int64(0), int64(32), "0.0.0.0", "0.0.0.0/32"},
		{int64(4294967296), int64(32), "", ""},
		{int64(-1), int64(32), "", ""},
		{float64(1), int64(32), "", ""},
		{"1.2.3.4", nil, "", ""},
		{nil, int64(32), "", ""},
		{"bad", int64(32), "", ""},
		{"::1", int64(32), "", ""},
		{"1.2.3.4", float64(24), "", ""},
	} {
		for _, fn := range []string{"kql_format_ipv4", "kql_format_ipv4_mask"} {
			want := tc.want
			if fn == "kql_format_ipv4_mask" {
				want = tc.mask
			}
			if got := querySQLiteFunction(t, store.DB(), "SELECT "+fn+"(?,?)", tc.input, tc.prefix); got != want {
				t.Fatalf("%s(%v,%v): got %v want %s", fn, tc.input, tc.prefix, got, want)
			}
		}
	}
	if got := querySQLiteFunction(t, store.DB(), "SELECT kql_format_ipv4_mask(?)", "10.1.2.3/16"); got != "10.1.0.0/16" {
		t.Fatal(got)
	}
	// Exercise every mask width and both edge bits independently of parsing
	// string examples. All output addresses must round-trip to the masked long.
	for prefix := int64(0); prefix <= 32; prefix++ {
		for _, value := range []int64{0, 1, 0x80000000, 0xabcdef01, 0xffffffff} {
			got := querySQLiteFunction(t, store.DB(), "SELECT kql_parse_ipv4(kql_format_ipv4(?,?))", value, prefix)
			want := int64((uint64(value) >> (32 - prefix)) << (32 - prefix))
			if got != want {
				t.Fatalf("%08x/%d: got %v want %d", value, prefix, got, want)
			}
		}
	}
	for _, query := range []string{"SELECT kql_parse_ipv4(NULL)", "SELECT kql_parse_ipv4_mask('1.2.3.4',NULL)", "SELECT kql_ipv4_netmask_suffix(NULL)"} {
		if got := querySQLiteFunction(t, store.DB(), query); got != nil {
			t.Fatal(fmt.Sprint(got))
		}
	}
}
