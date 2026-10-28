package database

import "testing"

func TestKQLToGUID(t *testing.T) {
	const want = "24fefbca-cf27-4d62-a623-249c2ad30c73"
	for _, input := range []string{want, "24FEFBCA-CF27-4D62-A623-249C2AD30C73", "24fefbcacf274d62a623249c2ad30c73", "{24fefbca-cf27-4d62-a623-249c2ad30c73}", "(24fefbca-cf27-4d62-a623-249c2ad30c73)", " " + want} {
		if got := kqlToGUID(input); got != want {
			t.Fatalf("%q -> %v", input, got)
		}
	}
	for _, input := range []any{"", "24fefbca-cf27-4d62-a623-249c2ad30c7g", "24fefbcacf27-4d62-a623-249c2ad30c73x", "{24fefbca-cf27-4d62-a623-249c2ad30c73)", "24fefbca_cf27_4d62_a623_249c2ad30c73", int64(1), nil} {
		if got := kqlToGUID(input); got != nil {
			t.Fatalf("%v accepted as %v", input, got)
		}
	}
}
