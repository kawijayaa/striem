package database

import "testing"

func TestIPv6RangeMembership(t *testing.T) {
	store, err := Open(t.TempDir() + "/membership.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, tc := range []struct {
		ip, network any
		want        any
	}{
		{"a5e:f127:8a9d:146d:e102:b5d3:c755:abcd", "a5e:f127:8a9d:146d:e102:b5d3:c755:0000/112", int64(1)},
		{"a5e:f127:8a9d:146d:e102:b5d3:c755:abcd", "a5e:f127:8a9d:146d:e102:b5d3:c755:abcd", int64(1)},
		{"a5e:f127:8a9d:146d:e102:b5d3:c755:abcd", "0:0:0:0:0:ffff:c0a8:ac/60", int64(0)},
		{"2001:db8::ffff", "2001:db8::abcd/112", int64(1)},
		{"2001:db8::1", "2001:db8::/128", int64(0)},
		{"ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", "::/0", int64(1)},
		{"192.168.1.255", "::ffff:c0a8:0100/120", int64(1)},
		{"192.168.2.1", "192.168.1.0/24", int64(0)},
		{"::1/0", "::/128", nil},
		{nil, "::/0", nil}, {"::1", nil, nil},
		{"::1", "::/129", nil}, {"bad", "::/0", nil},
		{"::1", "::/0,::1", nil}, {"fe80::1%eth0", "::/0", nil},
	} {
		if got := querySQLiteFunction(t, store.DB(), "SELECT kql_ipv6_is_in_range(?,?)", tc.ip, tc.network); got != tc.want {
			t.Fatalf("%v in %v: got %v want %v", tc.ip, tc.network, got, tc.want)
		}
	}
}

func FuzzIPv6RangeMembership(f *testing.F) {
	f.Add("2001:db8::abcd", "2001:db8::/112")
	f.Add("192.168.1.1", "::ffff:c0a8:0100/120")
	f.Fuzz(func(t *testing.T, ip, network string) {
		got := kqlIPv6IsInRange(ip, network)
		if got == nil {
			return
		}
		data, _, valid := ipv6Address(ip)
		base, prefix, validRange := ipv6Address(network)
		if !valid || !validRange {
			t.Fatal("invalid input accepted")
		}
		// Compare individual network bits independently of the production masker.
		want := true
		for bit := 0; bit < prefix; bit++ {
			if (data[bit/8]>>(7-bit%8))&1 != (base[bit/8]>>(7-bit%8))&1 {
				want = false
				break
			}
		}
		if got != want {
			t.Fatalf("membership got %v want %v", got, want)
		}
	})
}
