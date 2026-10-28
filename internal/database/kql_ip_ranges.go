package database

import (
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"slices"
	"strings"
	"unicode/utf8"
)

const maxIPRangeListBytes = 4 << 20
const maxIPRangeListItems = 65536

func kqlIPIsInAnyRange(address, ranges any, addressType string, ipv6 bool) (any, error) {
	ip, ok := address.(string)
	if addressType == "dynamic" {
		if !ok || len(ip) > 6*maxIPv6AddressBytes+2 || json.Unmarshal([]byte(ip), &ip) != nil {
			return nil, nil
		}
	}
	if !ok {
		return nil, nil
	}
	var host netip.Addr
	var data [16]byte
	if ipv6 {
		if strings.Contains(ip, "/") {
			return nil, nil
		}
		var valid bool
		data, _, valid = ipv6Address(ip)
		if !valid {
			return nil, nil
		}
	} else {
		var valid bool
		host, valid = parseIPv4Address(ip, false)
		if !valid {
			return nil, nil
		}
	}
	text, ok := ranges.(string)
	if !ok {
		return nil, nil
	}
	if len(text) > maxIPRangeListBytes {
		return nil, fmt.Errorf("IP range list exceeds byte limit")
	}
	if !utf8.ValidString(text) {
		return nil, nil
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return nil, nil
	}
	matched, count := false, 0
	for decoder.More() {
		// Decode into a string pointer to distinguish JSON null from empty string.
		var value *string
		if err := decoder.Decode(&value); err != nil || value == nil {
			return nil, nil
		}
		members := slices.Values([]string{*value})
		if !ipv6 {
			members = strings.SplitSeq(*value, ",")
		}
		for member := range members {
			var hit bool
			count++
			if count > maxIPRangeListItems {
				return nil, fmt.Errorf("IP range list exceeds item limit")
			}
			if ipv6 {
				base, prefix, valid := ipv6Address(member)
				if !valid {
					return nil, nil
				}
				hit = maskIPv6(data, prefix) == base
			} else {
				prefix, valid := parseIPv4Range(strings.TrimSpace(member))
				if !valid {
					return nil, nil
				}
				hit = prefix.Contains(host)
			}
			matched = matched || hit
		}
	}
	if _, err := decoder.Token(); err != nil {
		return nil, nil
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, nil
	}
	return matched, nil
}

func kqlIPv4IsInAnyRange(address, ranges any, addressType string) (any, error) {
	return kqlIPIsInAnyRange(address, ranges, addressType, false)
}
func kqlIPv6IsInAnyRange(address, ranges any, addressType string) (any, error) {
	return kqlIPIsInAnyRange(address, ranges, addressType, true)
}
