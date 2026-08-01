package database

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kawijayaa/striem/internal/eventtime"
	"github.com/kawijayaa/striem/internal/kql"
)

var explainBenchmarks = flag.Bool("explain", false, "print EXPLAIN QUERY PLAN for query benchmarks")
var queryBenchmarkDatabase = flag.String("query-db", "", "use an existing provisioned database for query benchmarks")
var queryBenchmarkFullText = flag.Bool("fulltext", false, "enable the opt-in FTS5 query path")

var explainedQueries sync.Map

func BenchmarkQuery(b *testing.B) {
	store, catalog := benchmarkQueryStore(b)
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	queries := []struct {
		name string
		kql  string
	}{
		{name: "Q1_JSONScalar", kql: `Suricata | where RawData.src_ip == "10.10.1.9" | take 100`},
		{name: "Q2_NestedTerm", kql: `Suricata | where RawData.alert.signature has "Heartbeat" | take 100`},
		{name: "Q3_JSONSummarize", kql: `Suricata | summarize c=count() by tostring(RawData.event_type)`},
		{name: "Q4_Search", kql: `Events | search "10.10.1.9" | take 100`},
		{name: "Q5_TimeIndex", kql: `Suricata | where TimeGenerated > ago(1d) | order by TimeGenerated desc | take 100`},
		{name: "Q6_Join", kql: `Sysmon | join kind=inner (Suricata) on Host | take 100`},
	}

	for _, query := range queries {
		b.Run(query.name, func(b *testing.B) {
			var compileOption kql.CompileOption = catalog
			if *queryBenchmarkFullText {
				compileOption = kql.CompileConfig{Tables: catalog, FullTextIndex: true}
			}
			compiled, err := kql.Compile(query.kql, now, compileOption)
			if err != nil {
				b.Fatal(err)
			}
			if *explainBenchmarks {
				printQueryPlan(b, store.DB(), query.name, compiled)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				rows, err := store.DB().QueryContext(b.Context(), compiled.SQL, compiled.Args...)
				if err != nil {
					b.Fatal(err)
				}
				consumeBenchmarkRows(b, rows)
			}
		})
	}
}

func benchmarkQueryStore(b *testing.B) (*Store, kql.TableCatalog) {
	b.Helper()
	path := *queryBenchmarkDatabase
	if path == "" {
		path = b.TempDir() + "/query-benchmark.db"
	}
	store, err := Open(path)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { store.Close() })
	if *queryBenchmarkDatabase != "" {
		datasets, err := store.ListDatasets(b.Context())
		if err != nil {
			b.Fatal(err)
		}
		catalog := make(kql.TableCatalog, len(datasets))
		for _, dataset := range datasets {
			catalog[dataset.Table] = dataset.ID
		}
		if *queryBenchmarkFullText {
			if err := store.ConfigureEventStorage(b.Context(), []string{"src_ip", "dest_ip", "alert.signature_id"}, true); err != nil {
				b.Fatal(err)
			}
			if err := store.SyncFullTextIndex(b.Context(), false); err != nil {
				b.Fatal(err)
			}
		}
		return store, catalog
	}
	tx, err := store.DB().BeginTx(b.Context(), nil)
	if err != nil {
		b.Fatal(err)
	}
	defer tx.Rollback()
	created := "2026-07-28T00:00:00Z"
	for _, dataset := range []struct {
		id    int
		name  string
		table string
		count int
	}{
		{id: 1, name: "Suricata benchmark", table: "Suricata", count: 50_000},
		{id: 2, name: "Sysmon benchmark", table: "Sysmon", count: 10_000},
	} {
		if _, err := tx.ExecContext(b.Context(), `INSERT INTO datasets(id, name, table_name, source, timestamp_path, event_count, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			dataset.id, dataset.name, dataset.table, strings.ToLower(dataset.table), "timestamp", dataset.count, created); err != nil {
			b.Fatal(err)
		}
	}
	statement, err := tx.PrepareContext(b.Context(), `INSERT INTO events(dataset_id, time_generated, source, event_type, host, username, message, raw_data) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		b.Fatal(err)
	}
	for index := 0; index < 50_000; index++ {
		timestamp := time.Date(2026, time.July, 20, 0, 0, 0, index, time.UTC)
		if index >= 49_000 {
			timestamp = time.Date(2026, time.July, 28, 0, 0, 0, index, time.UTC)
		}
		srcIP := fmt.Sprintf("10.10.%d.%d", index%255, index%253)
		if index%257 == 0 {
			srcIP = "10.10.1.9"
		}
		signature := "ET POLICY Routine traffic"
		if index%509 == 0 {
			signature = "ET MALWARE Heartbeat detected"
		}
		host := fmt.Sprintf("suricata-%d", index)
		if index < 10_000 {
			host = fmt.Sprintf("host-%d", index)
		}
		raw := fmt.Sprintf(`{"timestamp":%q,"event_type":"alert","src_ip":%q,"dest_ip":"192.0.2.%d","alert":{"signature_id":%d,"signature":%q},"network":{"protocol":"tcp","bytes":1024}}`,
			timestamp.Format(time.RFC3339Nano), srcIP, index%251, index%10_000, signature)
		if _, err := statement.ExecContext(b.Context(), 1, eventtime.Format(timestamp), "suricata", "alert", host, nil, signature, raw); err != nil {
			b.Fatal(err)
		}
	}
	for index := 0; index < 10_000; index++ {
		timestamp := time.Date(2026, time.July, 27, 0, 0, 0, index, time.UTC)
		host := fmt.Sprintf("host-%d", index)
		raw := fmt.Sprintf(`{"Event":{"System":{"TimeCreated":{"SystemTime":%q},"Computer":%q,"EventID":1},"EventData":{"Image":"powershell.exe","DestinationIp":"10.10.1.9"}}}`, timestamp.Format(time.RFC3339Nano), host)
		if _, err := statement.ExecContext(b.Context(), 2, eventtime.Format(timestamp), "sysmon", "1", host, fmt.Sprintf("user-%d", index%100), "Process Create", raw); err != nil {
			b.Fatal(err)
		}
	}
	if err := statement.Close(); err != nil {
		b.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	if err := store.ConfigureEventStorage(b.Context(), []string{"src_ip", "dest_ip", "alert.signature_id"}, *queryBenchmarkFullText); err != nil {
		b.Fatal(err)
	}
	if err := store.CreateEventIndexes(b.Context()); err != nil {
		b.Fatal(err)
	}
	if err := store.SyncFullTextIndex(b.Context(), *queryBenchmarkFullText); err != nil {
		b.Fatal(err)
	}
	return store, kql.TableCatalog{"Suricata": 1, "Sysmon": 2}
}

func consumeBenchmarkRows(b *testing.B, rows *sql.Rows) {
	b.Helper()
	columns, err := rows.Columns()
	if err != nil {
		b.Fatal(err)
	}
	values := make([]any, len(columns))
	pointers := make([]any, len(columns))
	for index := range values {
		pointers[index] = &values[index]
	}
	for rows.Next() {
		if err := rows.Scan(pointers...); err != nil {
			rows.Close()
			b.Fatal(err)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		b.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		b.Fatal(err)
	}
}

func printQueryPlan(b *testing.B, db *sql.DB, name string, compiled kql.CompiledQuery) {
	b.Helper()
	if _, loaded := explainedQueries.LoadOrStore(name, struct{}{}); loaded {
		return
	}
	rows, err := db.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+compiled.SQL, compiled.Args...)
	if err != nil {
		b.Fatal(err)
	}
	defer rows.Close()
	fmt.Fprintf(os.Stdout, "\n%s EXPLAIN QUERY PLAN\n", name)
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			b.Fatal(err)
		}
		fmt.Fprintf(os.Stdout, "%d|%d|%s\n", id, parent, detail)
	}
	if err := rows.Err(); err != nil {
		b.Fatal(err)
	}
}
