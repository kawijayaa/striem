package database

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"math/bits"
	"net/netip"
	"strconv"
)

// IPv4 conversion uses unsigned 32-bit network-order bits in a KQL long.
// Parsing is lexical only: no host lookup or external service is involved.
func ipv4Bits(value any) (uint32, int, bool) {
	text, ok := value.(string)
	if !ok {
		return 0, 0, false
	}
	prefix, ok := parseIPv4Range(text)
	if !ok {
		return 0, 0, false
	}
	address := prefix.Masked().Addr().As4()
	return binary.BigEndian.Uint32(address[:]), prefix.Bits(), true
}

func ipv4Prefix(value any) (int, bool) {
	number, ok := value.(int64)
	if !ok || number < 0 || number > 32 {
		return 0, false
	}
	return int(number), true
}

func maskedIPv4(bits uint32, prefix int) uint32 {
	return bits & (^uint32(0) << (32 - prefix))
}

func kqlParseIPv4(value any) any {
	bits, _, ok := ipv4Bits(value)
	if !ok {
		return nil
	}
	return int64(bits)
}

func kqlParseIPv4Mask(value, mask any) any {
	bits, inherited, ok := ipv4Bits(value)
	prefix, valid := ipv4Prefix(mask)
	if !ok || !valid {
		return nil
	}
	if inherited < prefix {
		prefix = inherited
	}
	return int64(maskedIPv4(bits, prefix))
}

func kqlIPv4NetmaskSuffix(value any) any {
	_, prefix, ok := ipv4Bits(value)
	if !ok {
		return nil
	}
	return int64(prefix)
}

func formatIPv4(value any, masks []any, withMask bool) string {
	if len(masks) > 1 {
		return ""
	}
	var bits uint32
	inherited := 32
	switch value := value.(type) {
	case string:
		var ok bool
		bits, inherited, ok = ipv4Bits(value)
		if !ok {
			return ""
		}
	case int64:
		if value < 0 || value > math.MaxUint32 {
			return ""
		}
		bits = uint32(value)
	default:
		return ""
	}
	prefix := inherited
	if len(masks) == 1 {
		explicit, ok := ipv4Prefix(masks[0])
		if !ok {
			return ""
		}
		if explicit < prefix {
			prefix = explicit
		}
	}
	var address [4]byte
	binary.BigEndian.PutUint32(address[:], maskedIPv4(bits, prefix))
	result := netip.AddrFrom4(address).String()
	if withMask {
		result += "/" + strconv.Itoa(prefix)
	}
	return result
}

func kqlFormatIPv4(value any, masks ...any) string     { return formatIPv4(value, masks, false) }
func kqlFormatIPv4Mask(value any, masks ...any) string { return formatIPv4(value, masks, true) }

func compareIPv4(left, right any, masks []any) (int64, bool) {
	if len(masks) > 1 {
		return 0, false
	}
	a, leftPrefix, leftOK := ipv4Bits(left)
	b, rightPrefix, rightOK := ipv4Bits(right)
	if !leftOK || !rightOK {
		return 0, false
	}
	prefix := leftPrefix
	if rightPrefix < prefix {
		prefix = rightPrefix
	}
	if len(masks) == 1 {
		explicit, ok := ipv4Prefix(masks[0])
		if !ok {
			return 0, false
		}
		if explicit < prefix {
			prefix = explicit
		}
	}
	a, b = maskedIPv4(a, prefix), maskedIPv4(b, prefix)
	// Compare unsigned address bits directly, avoiding signed overflow and
	// lexical ordering of dotted-decimal text.
	if a < b {
		return -1, true
	}
	if a > b {
		return 1, true
	}
	return 0, true
}

func kqlIPv4Compare(left, right any, masks ...any) any {
	order, ok := compareIPv4(left, right, masks)
	if !ok {
		return nil
	}
	return order
}

func kqlIPv4IsMatch(left, right any, masks ...any) any {
	order, ok := compareIPv4(left, right, masks)
	if !ok {
		return nil
	}
	return order == 0
}

func kqlIPv4RangeToCIDRList(startValue, endValue any) any {
	startText, startOK := startValue.(string)
	endText, endOK := endValue.(string)
	if !startOK || !endOK {
		return nil
	}
	startAddress, startOK := parseIPv4Address(startText, false)
	endAddress, endOK := parseIPv4Address(endText, false)
	if !startOK || !endOK {
		return nil
	}
	first, last := startAddress.As4(), endAddress.As4()
	start, end := uint64(binary.BigEndian.Uint32(first[:])), uint64(binary.BigEndian.Uint32(last[:]))
	if start > end {
		return nil
	}
	var ranges []string
	for start <= end {
		// Choose the largest aligned block that fits the remaining interval.
		// uint64 arithmetic also represents the full 2^32-address range and
		// the endpoint just beyond 255.255.255.255 without wrapping to zero.
		hostBits := bits.TrailingZeros32(uint32(start))
		remainingBits := bits.Len64(end-start+1) - 1
		if remainingBits < hostBits {
			hostBits = remainingBits
		}
		var address [4]byte
		binary.BigEndian.PutUint32(address[:], uint32(start))
		ranges = append(ranges, netip.PrefixFrom(netip.AddrFrom4(address), 32-hostBits).String())
		start += uint64(1) << uint(hostBits)
	}
	data, _ := json.Marshal(ranges)
	return string(data)
}
