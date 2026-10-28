package database

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"unicode/utf8"
)

const maxHashInputBytes = 4 << 20

func hashKQLScalar(value any, kind, algorithm string) (any, error) {
	var text string
	if blob, ok := value.([]byte); ok && blob == nil {
		value = nil
	}
	if value != nil {
		switch kind {
		case "datetime":
			var err error
			text, err = canonicalKQLDatetime(value)
			if err != nil {
				return nil, err
			}
		case "bool":
			number, ok := value.(int64)
			if !ok || (number != 0 && number != 1) {
				return nil, fmt.Errorf("hash boolean representation is unsupported")
			}
			text = strconv.FormatBool(number == 1)
		default:
			switch v := value.(type) {
			case string:
				text = v
			case int64:
				text = strconv.FormatInt(v, 10)
			case float64:
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return nil, fmt.Errorf("nonfinite hash input is unsupported")
				}
				text = strconv.FormatFloat(v, 'g', -1, 64)
			default:
				return nil, fmt.Errorf("hash scalar representation is unsupported")
			}
		}
	}
	if len(text) > maxHashInputBytes {
		return nil, fmt.Errorf("hash input exceeds byte limit")
	}
	if !utf8.ValidString(text) {
		return nil, nil
	}
	data := []byte(text)
	switch algorithm {
	case "md5":
		sum := md5.Sum(data)
		return hex.EncodeToString(sum[:]), nil
	case "sha1":
		sum := sha1.Sum(data)
		return hex.EncodeToString(sum[:]), nil
	case "sha256":
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:]), nil
	default:
		return nil, fmt.Errorf("unsupported hash algorithm")
	}
}

func kqlHashMD5(value any, kind string) (any, error)    { return hashKQLScalar(value, kind, "md5") }
func kqlHashSHA1(value any, kind string) (any, error)   { return hashKQLScalar(value, kind, "sha1") }
func kqlHashSHA256(value any, kind string) (any, error) { return hashKQLScalar(value, kind, "sha256") }
