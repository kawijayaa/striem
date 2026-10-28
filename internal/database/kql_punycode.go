package database

import (
	"strings"
	"unicode/utf8"
)

// Punycode (RFC 3492) as Kusto's punycode_* functions use it: plain encoding
// without IDNA case mapping, so labels keep their case and spaces. Failures
// return an empty string.

const (
	punycodeBase        = 36
	punycodeTMin        = 1
	punycodeTMax        = 26
	punycodeSkew        = 38
	punycodeDamp        = 700
	punycodeInitialBias = 72
	punycodeInitialN    = 128
	maxPunycodeBytes    = 1 << 16
)

func punycodeAdapt(delta, points int, first bool) int {
	if first {
		delta /= punycodeDamp
	} else {
		delta /= 2
	}
	delta += delta / points
	k := 0
	for delta > ((punycodeBase-punycodeTMin)*punycodeTMax)/2 {
		delta /= punycodeBase - punycodeTMin
		k += punycodeBase
	}
	return k + (punycodeBase-punycodeTMin+1)*delta/(delta+punycodeSkew)
}

func punycodeDigit(d int) byte {
	if d < 26 {
		return byte('a' + d)
	}
	return byte('0' + d - 26)
}

func punycodeThreshold(k, bias int) int {
	switch {
	case k <= bias:
		return punycodeTMin
	case k >= bias+punycodeTMax:
		return punycodeTMax
	}
	return k - bias
}

func punycodeEncode(input string) (string, bool) {
	if !utf8.ValidString(input) || len(input) > maxPunycodeBytes {
		return "", false
	}
	runes := []rune(input)
	var output strings.Builder
	for _, r := range runes {
		if r < 0x80 {
			output.WriteRune(r)
		}
	}
	basic := output.Len()
	handled := basic
	if basic > 0 {
		output.WriteByte('-')
	}
	n, delta, bias := punycodeInitialN, 0, punycodeInitialBias
	for handled < len(runes) {
		m := int(^uint(0) >> 1)
		for _, r := range runes {
			if int(r) >= n && int(r) < m {
				m = int(r)
			}
		}
		if (m-n)*(handled+1) > (1<<31)-1-delta {
			return "", false
		}
		delta += (m - n) * (handled + 1)
		n = m
		for _, r := range runes {
			if int(r) < n {
				delta++
			}
			if int(r) == n {
				q := delta
				for k := punycodeBase; ; k += punycodeBase {
					t := punycodeThreshold(k, bias)
					if q < t {
						break
					}
					output.WriteByte(punycodeDigit(t + (q-t)%(punycodeBase-t)))
					q = (q - t) / (punycodeBase - t)
				}
				output.WriteByte(punycodeDigit(q))
				bias = punycodeAdapt(delta, handled+1, handled == basic)
				delta = 0
				handled++
			}
		}
		delta++
		n++
	}
	return output.String(), true
}

func punycodeDecode(input string) (string, bool) {
	if len(input) > maxPunycodeBytes {
		return "", false
	}
	for i := 0; i < len(input); i++ {
		if input[i] >= 0x80 {
			return "", false
		}
	}
	var output []rune
	position := 0
	if last := strings.LastIndexByte(input, '-'); last >= 0 {
		output = []rune(input[:last])
		position = last + 1
	}
	n, i, bias := punycodeInitialN, 0, punycodeInitialBias
	for position < len(input) {
		old, w := i, 1
		for k := punycodeBase; ; k += punycodeBase {
			if position >= len(input) {
				return "", false
			}
			c := input[position]
			position++
			var digit int
			switch {
			case c >= 'a' && c <= 'z':
				digit = int(c - 'a')
			case c >= 'A' && c <= 'Z':
				digit = int(c - 'A')
			case c >= '0' && c <= '9':
				digit = int(c-'0') + 26
			default:
				return "", false
			}
			if digit > ((1<<31)-1-i)/w {
				return "", false
			}
			i += digit * w
			t := punycodeThreshold(k, bias)
			if digit < t {
				break
			}
			if w > ((1<<31)-1)/(punycodeBase-t) {
				return "", false
			}
			w *= punycodeBase - t
		}
		count := len(output) + 1
		bias = punycodeAdapt(i-old, count, old == 0)
		if i/count > (1<<31)-1-n {
			return "", false
		}
		n += i / count
		i %= count
		if n > utf8.MaxRune || (n >= 0xD800 && n <= 0xDFFF) || len(output) >= maxPunycodeBytes {
			return "", false
		}
		output = append(output, 0)
		copy(output[i+1:], output[i:])
		output[i] = rune(n)
		i++
	}
	return string(output), true
}

// splitDomainLabels splits a domain on the IDNA label separators.
func splitDomainLabels(domain string) []string {
	return strings.Split(strings.NewReplacer("。", ".", "．", ".", "｡", ".").Replace(domain), ".")
}

func kqlPunycodeFromString(value any) any {
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) {
		return nil
	}
	encoded, _ := punycodeEncode(text)
	return encoded
}

func kqlPunycodeToString(value any) any {
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) {
		return nil
	}
	decoded, _ := punycodeDecode(text)
	return decoded
}

func kqlPunycodeDomainToString(value any) any {
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) {
		return nil
	}
	labels := splitDomainLabels(text)
	for i, label := range labels {
		ascii := true
		for j := 0; j < len(label); j++ {
			ascii = ascii && label[j] < 0x80
		}
		if ascii {
			continue
		}
		encoded, ok := punycodeEncode(label)
		if !ok {
			return ""
		}
		labels[i] = "xn--" + encoded
	}
	return strings.Join(labels, ".")
}

func kqlPunycodeDomainFromString(value any) any {
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) {
		return nil
	}
	labels := strings.Split(text, ".")
	for i, label := range labels {
		if len(label) < 4 || !strings.EqualFold(label[:4], "xn--") {
			continue
		}
		decoded, ok := punycodeDecode(label[4:])
		if !ok {
			return ""
		}
		labels[i] = decoded
	}
	return strings.Join(labels, ".")
}
