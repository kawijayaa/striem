package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestQuotedScalarAndTabularLetNames(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`let ['some number']=20; range y from 0 to ['some number'] step 5`, `[[0] [5] [10] [15] [20]]`},
		{`let ['a.b']=dynamic({"x":113}); print value=['a.b'].x`, `[[113]]`},
		{`let ['where']=((Events | where n == 1)); ['where'] | project user`, `[[alice]]`},
		{`let ["T U"]=((print d=parse_json('{"a":127}'))); ["T U"] | project d.a`, `[[127]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got=%s want=%s", tc.query, got, tc.want)
		}
	}
	result, err := Compile(`let ['T U']=(print d=dynamic([1]),b=not(false)); ['T U'] | project d,b`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.DynamicColumns["d"]; !ok {
		t.Fatal("dynamic metadata lost")
	}
	if _, ok := result.BooleanColumns["b"]; !ok {
		t.Fatal("Boolean metadata lost")
	}
}
