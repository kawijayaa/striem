package database

import (
	"encoding/json"
	"strings"
	"testing"
)

func FuzzIPRangeLists(f *testing.F) {
	f.Add("127.0.0.1", "127.0.0.0/8", "10.0.0.0/8")
	f.Add("::1", "::/0", "bad")
	f.Fuzz(func(t *testing.T, ip, first, second string) {
		payload, err := json.Marshal([]string{first, second})
		if err != nil {
			t.Fatal(err)
		}
		for _, ipv6 := range []bool{false, true} {
			got, err := kqlIPIsInAnyRange(ip, string(payload), "string", ipv6)
			if len(payload) > maxIPRangeListBytes {
				if err == nil {
					t.Fatal("missing byte error")
				}
				continue
			}
			if err != nil {
				continue
			} // Expanded IPv4 comma lists can exceed the item budget.
			var a, b any
			if ipv6 {
				a = kqlIPv6IsInRange(ip, first)
				b = kqlIPv6IsInRange(ip, second)
			} else {
				a = kqlIPv4IsInRange(ip, first)
				b = kqlIPv4IsInRange(ip, second)
			}
			// The older single IPv4 helper has a smaller comma-list limit.
			if !ipv6 && (strings.Count(first, ",") >= maxIPv4Ranges || strings.Count(second, ",") >= maxIPv4Ranges || len(first) > maxIPv4RangeBytes || len(second) > maxIPv4RangeBytes) {
				continue
			}
			var want any
			if a != nil && b != nil {
				want = a == true || b == true
			}
			if got != want {
				t.Fatalf("ipv6=%v got %v want %v", ipv6, got, want)
			}
		}
	})
}

func TestIPRangeListValidation(t *testing.T) {
	escaped := `"` + strings.Repeat(`\u0030`, 4) + strings.Repeat(`\u003a`+strings.Repeat(`\u0030`, 4), 7) + `"`
	if got, err := kqlIPv6IsInAnyRange(escaped, `["::/0"]`, "dynamic"); err != nil || got != true {
		t.Fatalf("escaped dynamic address: %v %v", got, err)
	}
	for _, ipv6 := range []bool{false, true} {
		ip, network := "127.0.0.1", "127.0.0.0/8"
		if ipv6 {
			ip, network = "::1", "::/0"
		}
		for _, input := range []any{nil, "null", "{}", `[null]`, `[1]`, `[["x"]]`, `["bad"]`, `["` + network + `","bad"]`, `["` + network + `"] false`, `["` + network + `",]`, `["\ud800"]`} {
			got, err := kqlIPIsInAnyRange(ip, input, "string", ipv6)
			if err != nil || got != nil {
				t.Fatalf("invalid list %v: %v %v", input, got, err)
			}
		}
		got, err := kqlIPIsInAnyRange(ip, "[]", "string", ipv6)
		if err != nil || got != false {
			t.Fatalf("empty list: %v %v", got, err)
		}
		for _, count := range []int{129, 65536, 65537} {
			input := "[" + strings.Repeat(`"`+network+`",`, count-1) + `"` + network + `"]`
			got, err := kqlIPIsInAnyRange(ip, input, "string", ipv6)
			if count <= maxIPRangeListItems {
				if err != nil || got != true {
					t.Fatalf("%d items: %v %v", count, got, err)
				}
			} else if err == nil {
				t.Fatal("item limit not enforced")
			}
		}
		if _, err := kqlIPIsInAnyRange(ip, strings.Repeat(" ", maxIPRangeListBytes+1), "string", ipv6); err == nil {
			t.Fatal("byte limit not enforced")
		}
	}
}
