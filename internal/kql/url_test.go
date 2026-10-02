package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestURLParsingExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, name := range []string{"parse_url", "parseurl"} {
		query := `print d=` + name + `("scheme://username:password@host:1234/this/is/a/path?k1=v1&k2=v2#fragment") | project scheme=d.Scheme,host=d.Host,port=d.Port,path=d.Path,user=d.Username,password=d.Password,key=d.["Query Parameters"].k1,fragment=d.Fragment`
		if got := fmt.Sprint(featureRows(t, db, query)); got != "[[scheme host 1234 this/is/a/path username password v1 fragment]]" {
			t.Fatalf("%s: %s", name, got)
		}
	}
	for _, name := range []string{"parse_urlquery", "parseurlquery"} {
		query := `datatable(raw:string)["k1=v1&k2=v2&k3=v3","view=vs-2019&preserve-view=true"] | extend d=` + name + `(raw) | project k1=d.["Query Parameters"].k1,view=d.["Query Parameters"].view,preserve=d.["Query Parameters"].["preserve-view"]`
		if got := fmt.Sprint(featureRows(t, db, query)); got != "[[v1 <nil> <nil>] [<nil> vs-2019 true]]" {
			t.Fatalf("%s: %s", name, got)
		}
	}
	for _, name := range []string{"parse_url", "parseurl", "parse_urlquery", "parseurlquery"} {
		for _, input := range []string{``, `1`, `true`, `dynamic({})`, `"a","b"`} {
			if _, err := Compile(`print d=`+name+`(`+input+`)`, time.Now()); err == nil {
				t.Errorf("accepted invalid %s(%s)", name, input)
			}
		}
		compiled, err := Compile(`print raw="https://host/?a=b" | project `+name+`(raw)`, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(compiled.Columns) != "[Column1]" {
			t.Fatalf("%s: generated name %v", name, compiled.Columns)
		}
		if _, ok := compiled.DynamicColumns["Column1"]; !ok {
			t.Fatalf("%s: lost dynamic type", name)
		}
	}
}
