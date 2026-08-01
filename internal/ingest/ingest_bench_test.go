package ingest

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const benchmarkRecordCount = 50_000

func TestRepeatedEVTXFixture(t *testing.T) {
	seed, err := os.ReadFile(filepath.Join("testdata", "security-one-record.evtx"))
	if err != nil {
		t.Fatal(err)
	}
	generated, err := io.ReadAll(newRepeatedEVTX(seed, 1))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, seed) {
		t.Fatal("single-chunk generated EVTX differs from its seed")
	}
	result, err := New(openTestStore(t)).Import(t.Context(), newRepeatedEVTX(seed, 2), false, Mapping{
		Name: "repeated-evtx", Table: "RepeatedEVTX", Format: FormatEVTX,
		Source: "windows", TimestampPath: "System.TimeCreated.SystemTime",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Dataset.EventCount != 2 {
		t.Fatalf("imported %d repeated EVTX records, want 2", result.Dataset.EventCount)
	}
}

func TestSeekableEVTXCannotExceedMeasuredLimit(t *testing.T) {
	seed, err := os.ReadFile(filepath.Join("testdata", "security-one-record.evtx"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("STRIEM_MAX_INPUT_BYTES", strconv.Itoa(len(seed)))
	_, err = New(openTestStore(t)).Import(t.Context(), newRepeatedEVTX(seed, 2), false, Mapping{
		Name: "oversized-evtx", Table: "OversizedEVTX", Format: FormatEVTX,
		Source: "windows", TimestampPath: "System.TimeCreated.SystemTime",
	})
	if err == nil || !strings.Contains(err.Error(), "expanded input exceeds") {
		t.Fatalf("Import() error = %v, want expanded input limit", err)
	}
}

func BenchmarkImport(b *testing.B) {
	fixtures := []struct {
		name      string
		format    string
		input     func() io.Reader
		inputSize int64
		mapping   Mapping
	}{
		{
			name: "NDJSON", format: FormatJSON,
			mapping: Mapping{
				Source: "suricata", TimestampPath: "timestamp", TimestampFormat: "rfc3339",
				FieldPaths: map[string]string{"EventType": "event_type", "Host": "host", "Message": "alert.signature"},
			},
		},
		{
			name: "CSV", format: FormatCSV,
			mapping: Mapping{
				Source: "microsoft365", TimestampPath: "CreationDate", TimestampFormat: "rfc3339",
				FieldPaths: map[string]string{"EventType": "Operation", "Host": "Host", "User": "UserId", "Message": "Message"},
			},
		},
		{
			name: "EVTX", format: FormatEVTX,
			mapping: Mapping{
				Source: "windows", TimestampPath: "System.TimeCreated.SystemTime",
				FieldPaths: map[string]string{"EventType": "System.EventID.Value", "Host": "System.Computer"},
			},
		},
	}
	ndjson := benchmarkNDJSON(benchmarkRecordCount)
	fixtures[0].input = func() io.Reader { return bytes.NewReader(ndjson) }
	fixtures[0].inputSize = int64(len(ndjson))
	csv := benchmarkCSV(benchmarkRecordCount)
	fixtures[1].input = func() io.Reader { return bytes.NewReader(csv) }
	fixtures[1].inputSize = int64(len(csv))
	evtxSeed, err := os.ReadFile(filepath.Join("testdata", "security-one-record.evtx"))
	if err != nil {
		b.Fatal(err)
	}
	fixtures[2].input = func() io.Reader { return newRepeatedEVTX(evtxSeed, benchmarkRecordCount) }
	fixtures[2].inputSize = int64(0x1000 + benchmarkRecordCount*0x10000)

	for _, fixture := range fixtures {
		b.Run(fixture.name, func(b *testing.B) {
			if fixture.format == FormatEVTX {
				b.Setenv("STRIEM_MAX_INPUT_BYTES", strconv.FormatInt(fixture.inputSize, 10))
			}
			store := openTestStore(b)
			if err := store.DropEventIndexes(b.Context()); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(fixture.inputSize)
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				mapping := fixture.mapping
				mapping.Name = fmt.Sprintf("benchmark-%s-%d", strings.ToLower(fixture.name), iteration)
				mapping.Table = fmt.Sprintf("Benchmark%s%d", fixture.name, iteration)
				mapping.Format = fixture.format
				result, err := New(store).Import(b.Context(), fixture.input(), false, mapping)
				if err != nil {
					b.Fatal(err)
				}
				if result.Dataset.EventCount != benchmarkRecordCount {
					b.Fatalf("imported %d records, want %d", result.Dataset.EventCount, benchmarkRecordCount)
				}
				b.StopTimer()
				if _, err := store.DB().ExecContext(b.Context(), "DELETE FROM datasets WHERE id = ?", result.Dataset.ID); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
			b.StopTimer()
			runtime.ReadMemStats(&after)
			records := float64(b.N * benchmarkRecordCount)
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/records, "ns/record")
			b.ReportMetric(float64(after.TotalAlloc-before.TotalAlloc)/records, "B/record")
		})
	}
}

func benchmarkNDJSON(count int) []byte {
	var output bytes.Buffer
	output.Grow(count * 240)
	for index := 0; index < count; index++ {
		fmt.Fprintf(&output, `{"timestamp":"2026-07-28T00:00:00Z","event_type":"alert","src_ip":"10.10.%d.%d","dest_ip":"192.0.2.%d","host":"sensor-%d","alert":{"signature_id":%d,"signature":"ET MALWARE Heartbeat detected"},"payload":[1,2,3]}`+"\n",
			index%255, index%253, index%251, index%100, index%10_000)
	}
	return output.Bytes()
}

func benchmarkCSV(count int) []byte {
	var output bytes.Buffer
	output.Grow(count * 180)
	output.WriteString("CreationDate,Operation,Host,UserId,Message,AuditData\n")
	for index := 0; index < count; index++ {
		fmt.Fprintf(&output, `2026-07-28T00:00:00Z,UserLoggedIn,pc-%d,user-%d,Successful login,"{""ClientIP"":""198.51.100.%d""}"`+"\n",
			index%1000, index%5000, index%251)
	}
	return output.Bytes()
}

type repeatedEVTX struct {
	seed       []byte
	chunkCount int64
	offset     int64
}

func newRepeatedEVTX(seed []byte, chunkCount int) *repeatedEVTX {
	return &repeatedEVTX{seed: seed, chunkCount: int64(chunkCount)}
}

func (reader *repeatedEVTX) Read(buffer []byte) (int, error) {
	const headerSize = int64(0x1000)
	const chunkSize = int64(0x10000)
	logicalSize := headerSize + reader.chunkCount*chunkSize
	if reader.offset >= logicalSize {
		return 0, io.EOF
	}
	written := 0
	for len(buffer) > 0 && reader.offset < logicalSize {
		var source []byte
		if reader.offset < headerSize {
			source = reader.seed[reader.offset:headerSize]
		} else {
			chunkOffset := (reader.offset - headerSize) % chunkSize
			source = reader.seed[headerSize+chunkOffset : headerSize+chunkSize]
		}
		count := copy(buffer, source)
		reader.offset += int64(count)
		written += count
		buffer = buffer[count:]
	}
	return written, nil
}

func (reader *repeatedEVTX) Seek(offset int64, whence int) (int64, error) {
	const headerSize = int64(0x1000)
	const chunkSize = int64(0x10000)
	logicalSize := headerSize + reader.chunkCount*chunkSize
	switch whence {
	case io.SeekStart:
		reader.offset = offset
	case io.SeekCurrent:
		reader.offset += offset
	case io.SeekEnd:
		reader.offset = logicalSize + offset
	default:
		return 0, fmt.Errorf("invalid seek origin %d", whence)
	}
	if reader.offset < 0 || reader.offset > logicalSize {
		return 0, fmt.Errorf("seek offset %d outside EVTX fixture", reader.offset)
	}
	return reader.offset, nil
}
