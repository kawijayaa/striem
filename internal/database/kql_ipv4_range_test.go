package database

import (
	"encoding/binary"
	"encoding/json"
	"net/netip"
	"testing"
)

func TestIPv4RangeCIDRCover(t *testing.T) {
	store, err := Open(t.TempDir() + "/ipv4-range.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, tc := range []struct {
		start, end string
		want       any
	}{
		{"1.1.128.0", "1.1.140.255", `["1.1.128.0/21","1.1.136.0/22","1.1.140.0/24"]`},
		{"0.0.0.0", "255.255.255.255", `["0.0.0.0/0"]`},
		{"0.0.0.0", "0.0.0.0", `["0.0.0.0/32"]`},
		{"255.255.255.255", "255.255.255.255", `["255.255.255.255/32"]`},
		{"127.255.255.255", "128.0.0.0", `["127.255.255.255/32","128.0.0.0/32"]`},
		{"10.0.0.0", "10.255.255.255", `["10.0.0.0/8"]`},
		{"192.168.1.1", "192.168.1.6", `["192.168.1.1/32","192.168.1.2/31","192.168.1.4/31","192.168.1.6/32"]`},
		{"192.168.1.6", "192.168.1.1", nil},
		{"bad", "1.2.3.4", nil},
		{"1.2.3.4", "bad", nil},
		{"1.2.3.4/24", "1.2.3.5", nil},
		{"::ffff:1.2.3.4", "1.2.3.5", nil},
	} {
		if got := querySQLiteFunction(t, store.DB(), "SELECT kql_ipv4_range_to_cidr_list(?,?)", tc.start, tc.end); got != tc.want {
			t.Fatalf("%s..%s: got %v want %v", tc.start, tc.end, got, tc.want)
		}
	}
	for _, pair := range [][2]any{{nil, "1.2.3.4"}, {"1.2.3.4", nil}, {int64(1), "1.2.3.4"}} {
		if got := querySQLiteFunction(t, store.DB(), "SELECT kql_ipv4_range_to_cidr_list(?,?)", pair[0], pair[1]); got != nil {
			t.Fatalf("invalid endpoints accepted: %v", got)
		}
	}
	// A maximally fragmented 32-bit interval still needs only 62 blocks.
	verifyCIDRCover(t, 1, 0xfffffffe)
}

func verifyCIDRCover(t *testing.T, start, end uint32) {
	t.Helper()
	got := kqlIPv4RangeToCIDRList(kqlFormatIPv4(int64(start)), kqlFormatIPv4(int64(end)))
	if start > end {
		if got != nil {
			t.Fatal("reversed range accepted")
		}
		return
	}
	text, ok := got.(string)
	if !ok {
		t.Fatal("valid range did not produce JSON")
	}
	var ranges []string
	if err := json.Unmarshal([]byte(text), &ranges); err != nil || len(ranges) < 1 || len(ranges) > 62 {
		t.Fatalf("invalid bounded cover: %d %v", len(ranges), err)
	}
	if start == 1 && end == 0xfffffffe && len(ranges) != 62 {
		t.Fatalf("maximally fragmented cover has %d blocks, want 62", len(ranges))
	}
	cursor := uint64(start)
	for i, value := range ranges {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || !prefix.Addr().Is4() || prefix.Masked() != prefix {
			t.Fatalf("invalid prefix: %s", value)
		}
		bytes := prefix.Addr().As4()
		first := uint64(binary.BigEndian.Uint32(bytes[:]))
		size := uint64(1) << uint(32-prefix.Bits())
		if first != cursor || first+size-1 > uint64(end) {
			t.Fatalf("cover gap/overlap: %s", value)
		}
		// Every emitted block must be maximal for its position and endpoint.
		if prefix.Bits() > 0 {
			parent := netip.PrefixFrom(prefix.Addr(), prefix.Bits()-1).Masked()
			parentBytes := parent.Addr().As4()
			parentFirst := uint64(binary.BigEndian.Uint32(parentBytes[:]))
			if parentFirst == first && parentFirst+2*size-1 <= uint64(end) {
				t.Fatalf("nonmaximal block %d: %s", i, value)
			}
		}
		cursor += size
	}
	if cursor != uint64(end)+1 {
		t.Fatal("cover omitted endpoint")
	}
}

func FuzzIPv4RangeCIDRCover(f *testing.F) {
	for _, pair := range [][2]uint32{{0, 0xffffffff}, {1, 0xfffffffe}, {0x01018000, 0x01018cff}, {0xffffffff, 0xffffffff}, {1, 0}} {
		f.Add(pair[0], pair[1])
	}
	f.Fuzz(func(t *testing.T, start, end uint32) { verifyCIDRCover(t, start, end) })
}
