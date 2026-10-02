package database

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParseXMLDocumentedShapes(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`<e/>`, `{"e":null}`},
		{`<e>text</e>`, `{"e":"text"}`},
		{`<e name="value"/>`, `{"e":{"@name":"value"}}`},
		{`<e name="value">text</e>`, `{"e":{"#text":"text","@name":"value"}}`},
		{`<e> <a>text</a> <b>text</b> </e>`, `{"e":{"a":"text","b":"text"}}`},
		{`<e> <a>text</a> <a>text</a> <a/> </e>`, `{"e":{"a":["text","text",null]}}`},
		{`<e> text <a>text</a> </e>`, `{"e":{"#text":"text","a":"text"}}`},
		{`<?xml version="1.0"?><e><!-- ignored -->text<?p ignored?></e>`, `{"e":"text"}`},
		{`<e>&amp;&lt;&#233;&#x41;</e>`, `{"e":"\u0026\u003céA"}`},
		{"\ufeff<e>text</e>", `{"e":"text"}`},
	} {
		got, err := kqlParseXML(tc.input)
		if err != nil || got != tc.want {
			t.Fatalf("%s: %v %v want=%s", tc.input, got, err, tc.want)
		}
	}
}

func TestParseXMLInvalidAndResourceInputs(t *testing.T) {
	for _, input := range []any{nil, "", "text", `<a><b></a></b>`, `<a/> <b/>`, `<a>`, `<a/>tail`, `<a x="1" x="2"/>`, `<a>&unknown;</a>`, "<a>\x00</a>", string([]byte{0xff}), strings.Repeat("a", maxDynamicInputBytes+1)} {
		if got, err := kqlParseXML(input); got != nil || err != nil {
			t.Fatalf("invalid %T input: %v %v", input, got, err)
		}
	}
	if _, err := kqlParseXML(int64(1)); err == nil {
		t.Fatal("missing runtime type diagnostic")
	}
	maximum := "<e>" + strings.Repeat("a", maxDynamicInputBytes-7) + "</e>"
	if got, err := kqlParseXML(maximum); got == nil || err != nil {
		t.Fatalf("maximum input: present=%v err=%v", got != nil, err)
	}
	if _, err := kqlParseXML("<e>" + strings.Repeat(">", maxDynamicInputBytes-7) + "</e>"); err == nil {
		t.Fatal("missing encoded output size diagnostic")
	}
	for depth := maxXMLDepth; depth <= maxXMLDepth+1; depth++ {
		got, err := kqlParseXML(strings.Repeat("<e>", depth) + strings.Repeat("</e>", depth))
		if depth == maxXMLDepth && (got == nil || err != nil) || depth > maxXMLDepth && err == nil {
			t.Fatalf("depth=%d present=%v err=%v", depth, got != nil, err)
		}
	}
}

// Namespace, whitespace, CDATA and type conversion require Kusto comparison.
func TestParseXMLCharacterization(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`<e> 123 </e>`, `{"e":"123"}`},
		{`<e><![CDATA[ignored]]></e>`, `{"e":null}`},
		{`<e>before<a/>after</e>`, `{"e":{"#text":"beforeafter","a":null}}`},
		{`<x:e xmlns:x="urn:example" x:attr="value"><x:a/></x:e>`, `{"x:e":{"@x:attr":"value","@xmlns:x":"urn:example","x:a":null}}`},
	} {
		if got, err := kqlParseXML(tc.input); got != tc.want || err != nil {
			t.Fatalf("%s: %v %v want=%s", tc.input, got, err, tc.want)
		}
	}
}

func TestParseXMLDoesNotResolveEntities(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Write([]byte("secret"))
	}))
	defer server.Close()
	file := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(file, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{server.URL, "file://" + file} {
		input := `<!DOCTYPE e [<!ENTITY x SYSTEM "` + source + `">]><e>&x;</e>`
		if got, err := kqlParseXML(input); got != nil || err != nil {
			t.Fatalf("external entity: %v %v", got, err)
		}
	}
	if got, err := kqlParseXML(`<!DOCTYPE e [<!ENTITY x "secret">]><e>&x;</e>`); got != nil || err != nil {
		t.Fatalf("internal entity: %v %v", got, err)
	}
	if requests.Load() != 0 {
		t.Fatal("XML parser made an external request")
	}
}
