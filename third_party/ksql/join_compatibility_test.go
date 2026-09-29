package ksql_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
)

func joinFixture(t *testing.T) (*sql.DB, testCatalog) {
	t.Helper()
	db, err := sql.Open(sqliteTestDriver, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`CREATE TABLE l (Key TEXT, K2 INTEGER, L INTEGER, Extra INTEGER, __ksql_join_rank INTEGER);
 CREATE TABLE r (Key TEXT, K2 INTEGER, R INTEGER);
 INSERT INTO l VALUES ('a',1,10,100,99),('a',1,20,200,98),('a',2,30,300,97),('b',1,40,400,96),(NULL,1,50,500,95);
 INSERT INTO r VALUES ('a',1,1000),('a',1,2000),('a',2,3000),('c',1,4000),(NULL,1,5000);`)
	if err != nil {
		t.Fatal(err)
	}
	return db, testCatalog{
		"Left":  {Name: "l", Schema: ksql.Schema{Columns: []ksql.Column{{Name: "Key", Type: ksql.TypeString}, {Name: "K2", Type: ksql.TypeLong}, {Name: "L", Type: ksql.TypeLong}, {Name: "Extra", Type: ksql.TypeLong}, {Name: "__ksql_join_rank", Type: ksql.TypeLong}}}},
		"Right": {Name: "r", Schema: ksql.Schema{Columns: []ksql.Column{{Name: "Key", Type: ksql.TypeString}, {Name: "K2", Type: ksql.TypeLong}, {Name: "R", Type: ksql.TypeLong}}}},
	}
}

func TestSQLiteJoinCompatibility(t *testing.T) {
	db, catalog := joinFixture(t)
	compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(catalog))
	for _, c := range []struct{ query, want string }{
		{`Left | join kind=rightsemi (Right) on Key,K2 | project R | order by R asc`, `[[1000] [2000] [3000]]`},
		{`Left | join kind=rightanti (Right) on Key,K2 | project R | order by R asc`, `[[4000] [5000]]`},
		{`Left | join kind=rightsemi (Right) on $right.Key == $left.Key and $left.K2 == $right.K2 | count`, `[[3]]`},
		{`Left | where L < 0 | join kind=rightanti (Right) on Key | count`, `[[5]]`},
		{`Left | join kind=rightsemi (Right | where R < 0) on Key | count`, `[[0]]`},
		{`Left | join kind=inner (Right) on Key,K2 | count`, `[[5]]`},
		{`Left | join (Right) on Key,K2 | count`, `[[3]]`},
		{`Left | join kind=innerunique (Right) on $left.Key == $right.Key and $right.K2 == $left.K2 | count`, `[[3]]`},
		{`Left | where L < 0 | join (Right) on Key,K2 | count`, `[[0]]`},
		{`let Selected=Left | where L > 0; Selected | join (Right) on Key,K2 | count`, `[[3]]`},
	} {
		t.Run(c.query, func(t *testing.T) {
			result := compiler.Compile(c.query)
			if got := fmt.Sprint(queryRows(t, db, result)); got != c.want {
				t.Fatalf("got %s want %s SQL=%s", got, c.want, result.SQL)
			}
		})
	}
	// Deduplication must preserve a complete representative row, not independent
	// aggregates of each column, and it must hide the internal window column.
	result := compiler.Compile(`Left | join (Right) on Key,K2 | project L,Extra,R,__ksql_join_rank | order by R asc`)
	rows := queryRows(t, db, result)
	if len(rows) != 3 || len(result.Columns) != 4 {
		t.Fatalf("unexpected shape: %v %#v", rows, result.Columns)
	}
	if rows[0][0] != rows[1][0] {
		t.Fatalf("dedup selected different left rows for the same key: %v", rows)
	}
	for _, row := range rows {
		if row[1].(int64) != row[0].(int64)*10 {
			t.Fatalf("incoherent representative row: %v", row)
		}
	}
	for _, kind := range []string{"rightsemi", "rightanti"} {
		result := compiler.Compile(`Left | join kind=` + kind + ` (Right) on Key`)
		if !result.OK() || len(result.Columns) != 3 || result.Columns[2].Name != "R" {
			t.Fatalf("right schema not preserved: %#v", result)
		}
	}
}

func TestInnerUniqueDialectRendering(t *testing.T) {
	_, catalog := joinFixture(t)
	for _, target := range []dialect.Dialect{dialect.SQLite(), dialect.PostgreSQL(), dialect.MySQL(), dialect.SQLServer()} {
		t.Run(target.Name(), func(t *testing.T) {
			result := ksql.New(target, ksql.WithCatalog(catalog)).Compile(`Left | join (Right) on Key,K2`)
			if !result.OK() {
				t.Fatal(result.Diagnostics)
			}
			if !strings.Contains(result.SQL, "ROW_NUMBER() OVER (PARTITION BY") || !strings.Contains(result.SQL, "INNER JOIN") {
				t.Fatal(result.SQL)
			}
		})
	}
}
