package database

import (
	"bytes"
	"net/netip"
	"strconv"
	"strings"
)

const maxIPv6AddressBytes = 49 // longest dotted-tail IPv6 spelling plus /128

func ipv6Address(value any) ([16]byte, int, bool) {
	var zero [16]byte
	text, ok := value.(string)
	if !ok || len(text) == 0 || len(text) > maxIPv6AddressBytes {
		return zero, 0, false
	}
	addressText, suffix, hasSuffix := strings.Cut(text, "/")
	address, err := netip.ParseAddr(addressText)
	if err != nil || address.Zone() != "" {
		return zero, 0, false
	}
	data := address.As16()
	plainIPv4 := address.Is4()
	// Kusto's documented ::dotted-IPv4 form normalizes to IPv4-mapped
	// bytes. Other dotted-tail IPv6 networks keep their actual high bits.
	dottedMapped := false
	if !plainIPv4 && strings.Contains(addressText, ".") {
		compatible := true
		for _, b := range data[:12] {
			if b != 0 {
				compatible = false
				break
			}
		}
		if compatible {
			data[10], data[11] = 0xff, 0xff
		}
		dottedMapped = compatible || address.Is4In6()
	}
	prefix := 128
	if hasSuffix {
		if len(suffix) < 1 || len(suffix) > 3 {
			return zero, 0, false
		}
		for i := range suffix {
			if suffix[i] < '0' || suffix[i] > '9' {
				return zero, 0, false
			}
		}
		prefix, err = strconv.Atoi(suffix)
		if err != nil || prefix > 128 || (plainIPv4 && prefix > 32) {
			return zero, 0, false
		}
		if plainIPv4 || (dottedMapped && prefix <= 32) {
			prefix += 96
		}
	}
	return maskIPv6(data, prefix), prefix, true
}

func maskIPv6(data [16]byte, prefix int) [16]byte {
	return netip.PrefixFrom(netip.AddrFrom16(data), prefix).Masked().Addr().As16()
}

func ipv6Prefix(value any) (int, bool) {
	number, ok := value.(int64)
	if !ok || number < 0 || number > 128 {
		return 0, false
	}
	return int(number), true
}

func expandedIPv6(data [16]byte) string {
	const hex = "0123456789abcdef"
	var text [39]byte
	position := 0
	for i, b := range data {
		if i > 0 && i%2 == 0 {
			text[position] = ':'
			position++
		}
		text[position], text[position+1] = hex[b>>4], hex[b&15]
		position += 2
	}
	return string(text[:])
}

func kqlParseIPv6(value any) string {
	data, _, ok := ipv6Address(value)
	if !ok {
		return ""
	}
	return expandedIPv6(data)
}

func kqlParseIPv6Mask(value, mask any) string {
	data, inherited, ok := ipv6Address(value)
	prefix, valid := ipv6Prefix(mask)
	if !ok || !valid {
		return ""
	}
	if inherited < prefix {
		prefix = inherited
	}
	return expandedIPv6(maskIPv6(data, prefix))
}

func compareIPv6(left, right any, masks []any) (int64, bool) {
	if len(masks) > 1 {
		return 0, false
	}
	a, leftPrefix, leftOK := ipv6Address(left)
	b, rightPrefix, rightOK := ipv6Address(right)
	if !leftOK || !rightOK {
		return 0, false
	}
	prefix := leftPrefix
	if rightPrefix < prefix {
		prefix = rightPrefix
	}
	if len(masks) == 1 {
		explicit, ok := ipv6Prefix(masks[0])
		if !ok {
			return 0, false
		}
		if explicit < prefix {
			prefix = explicit
		}
	}
	a, b = maskIPv6(a, prefix), maskIPv6(b, prefix)
	return int64(bytes.Compare(a[:], b[:])), true
}

func kqlIPv6Compare(left, right any, masks ...any) any {
	order, ok := compareIPv6(left, right, masks)
	if !ok {
		return nil
	}
	return order
}

func kqlIPv6IsMatch(left, right any, masks ...any) any {
	order, ok := compareIPv6(left, right, masks)
	if !ok {
		return nil
	}
	return order == 0
}

// Membership applies only the range's prefix. A candidate must be a host
// address; accepting its own prefix would silently discard significant bits.
func kqlIPv6IsInRange(addressValue, rangeValue any) any {
	address, ok := addressValue.(string)
	if !ok || strings.Contains(address, "/") {
		return nil
	}
	data, _, valid := ipv6Address(address)
	network, prefix, rangeValid := ipv6Address(rangeValue)
	if !valid || !rangeValid {
		return nil
	}
	return maskIPv6(data, prefix) == network
}
