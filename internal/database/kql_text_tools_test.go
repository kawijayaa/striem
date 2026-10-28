package database

import "testing"

// xxhash64 against Kusto's documented hash() values and reference vectors,
// including inputs over 32 bytes.
func TestXXHash64(t *testing.T) {
	if got := int64(xxhash64([]byte("World"))); got != 1846988464401551951 {
		t.Fatalf("hash(World) = %d", got)
	}
	if got := xxhash64(nil); got != 0xef46db3751d8e999 {
		t.Fatalf("hash('') = %x", got)
	}
	if got := xxhash64([]byte("Nobody inspects the spammish repetition")); got != 0xfbcea83c8a378bf1 {
		t.Fatalf("hash(long) = %x", got)
	}
	if got := kqlHash("2015-01-01T00:00:00.000000000Z", "datetime", nil); got != int64(1380966698541616202) {
		t.Fatalf("hash(datetime) = %v", got)
	}
}
