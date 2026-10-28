package database

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/adler32"
	"hash/crc32"
	"io"
	"strings"
	"unicode/utf8"
)

const maxCompressionTextBytes = 4 << 20
const maxCompressionWireBytes = maxCompressionTextBytes + 5*((maxCompressionTextBytes+65534)/65535) + 18

func compressKQLText(value any, gz bool) (any, error) {
	text, ok := value.(string)
	if !ok || !utf8.ValidString(text) {
		return nil, nil
	}
	if len(text) > maxCompressionTextBytes {
		return nil, fmt.Errorf("compression input exceeds byte limit")
	}
	data := []byte(text)
	result := make([]byte, 0, len(data)+len(data)/65535*5+32)
	if gz {
		result = append(result, 0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 0, 255)
	} else {
		result = append(result, 0x78, 0x01)
	}
	// Microsoft's published examples use stored DEFLATE blocks. Emit that
	// deterministic format, splitting only at the RFC 1951 block-size bound.
	rest := data
	for {
		size := min(len(rest), 65535)
		final := byte(0)
		if size == len(rest) {
			final = 1
		}
		result = append(result, final)
		result = binary.LittleEndian.AppendUint16(result, uint16(size))
		result = binary.LittleEndian.AppendUint16(result, ^uint16(size))
		result = append(result, rest[:size]...)
		rest = rest[size:]
		if final == 1 {
			break
		}
	}
	if gz {
		result = binary.LittleEndian.AppendUint32(result, crc32.ChecksumIEEE(data))
		result = binary.LittleEndian.AppendUint32(result, uint32(len(data)))
	} else {
		result = binary.BigEndian.AppendUint32(result, adler32.Checksum(data))
	}
	return base64.StdEncoding.EncodeToString(result), nil
}

func decompressKQLText(value any, gz bool) (any, error) {
	text, ok := value.(string)
	if !ok {
		return nil, nil
	}
	if len(text) > base64.StdEncoding.EncodedLen(maxCompressionWireBytes) {
		return nil, fmt.Errorf("compressed input exceeds byte limit")
	}
	if strings.ContainsAny(text, "\r\n") {
		return nil, nil
	}
	data, err := base64.StdEncoding.Strict().DecodeString(text)
	if err != nil || base64.StdEncoding.EncodeToString(data) != text {
		return nil, nil
	}
	if len(data) > maxCompressionWireBytes {
		return nil, fmt.Errorf("compressed input exceeds byte limit")
	}
	input := bytes.NewReader(data)
	var reader io.ReadCloser
	if gz {
		if len(data) < 18 || data[0] != 0x1f || data[1] != 0x8b || data[2] != 8 || data[3] != 0 {
			return nil, nil
		}
		r, err := gzip.NewReader(input)
		if err != nil {
			return nil, nil
		}
		r.Multistream(false)
		reader = r
	} else {
		// Kusto documents only a 15-bit window and no preset dictionary.
		if len(data) < 6 || data[0] != 0x78 || data[1]&0x20 != 0 {
			return nil, nil
		}
		r, err := zlib.NewReader(input)
		if err != nil {
			return nil, nil
		}
		reader = r
	}
	defer reader.Close()
	output, err := io.ReadAll(io.LimitReader(reader, maxCompressionTextBytes+1))
	if len(output) > maxCompressionTextBytes {
		return nil, fmt.Errorf("decompressed output exceeds byte limit")
	}
	if err != nil || input.Len() != 0 || !utf8.Valid(output) {
		return nil, nil
	}
	return string(output), nil
}

func kqlGzipCompress(value any) (any, error)   { return compressKQLText(value, true) }
func kqlGzipDecompress(value any) (any, error) { return decompressKQLText(value, true) }
func kqlZlibCompress(value any) (any, error)   { return compressKQLText(value, false) }
func kqlZlibDecompress(value any) (any, error) { return decompressKQLText(value, false) }
