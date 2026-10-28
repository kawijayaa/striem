package kql

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestStringJoinExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print s=strcat_delim("-",1,"2","A",true),n=strcat_delim("|",long(null),"x"),d=strcat_delim("|",datetime(2020-01-01),"x")`, `[[1-2-A-true |x 2020-01-01T00:00:00.0000000Z|x]]`},
		{`print s=strrep(datetime(2020-01-01),2,"|"),d=strrep("x",2,datetime(2020-01-01))`, `[[2020-01-01T00:00:00.0000000Z|2020-01-01T00:00:00.0000000Z x2020-01-01T00:00:00.0000000Zx]]`},
		{`print s=strcat_array(dynamic([1,2,3]),"->"),q=strcat_array(dynamic([1,2,3]),'"')`, `[[1->2->3 1"2"3]]`},
		{`print a=strrep("ABC",2),b=strrep(123,3,"."),c=strrep(true,2,false)`, `[[ABCABC 123.123.123 truefalsetrue]]`},
		{`print a=strcat_array(dynamic(["😀",null,"é"]),"|"),b=strcat_array(dynamic([]),"|"),c=strcat_array(dynamic([9223372036854775807]),",")`, `[[😀||é  9223372036854775807]]`},
		{`datatable(n:long)[1,2,3]|project s=strrep("x",n,"-")`, `[[x] [x-x] [x-x-x]]`},
		{`print s=strcat_array(range(1,3),",")`, `[[1,2,3]]`},
		{`print n=strlen(strrep("x",1025)),z=strrep("x",0),e=strrep("",3,"-")`, `[[1024 <nil> --]]`},
		{`let f=(s:string){strrep(s,2,"-")};print s=f("😀")`, `[[😀-😀]]`},
		{`print a=strrep("x",1),b=strcat_array(dynamic([]),",")|getschema|project ColumnType`, `[[string] [string]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
	if got := fmt.Sprint(featureRows(t, db, `print s=strcat_delim("-",`+strings.Repeat(`"x",`, 63)+`"x")`)); got != "[["+strings.TrimSuffix(strings.Repeat("x-", 64), "-")+"]]" {
		t.Fatal("64 scalar values failed")
	}
	for _, call := range []string{`strcat_delim("-")`, `strcat_delim("-",1)`, `strcat_delim(1,"x","y")`, `strcat_delim("-",dynamic([]),"x")`, `strcat_delim("-",` + strings.Repeat(`"x",`, 64) + `"x")`, `strcat_array(dynamic([]))`, `strcat_array("[]",",")`, `strcat_array(dynamic([]),1)`, `strrep("a")`, `strrep("a",1.5)`, `strrep(dynamic([]),2)`, `strrep("a",2,"b","c")`} {
		if _, err := Compile("print s="+call, time.Now()); err == nil {
			t.Fatalf("invalid signature accepted: %s", call)
		}
	}
}
