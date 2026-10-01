package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestStringLiteralForms(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print x='hello\nworld'`, "[[hello\nworld]]"},
		{`print x='it\'s "quoted"',y="it\'s \"quoted\""`, `[[it's "quoted" it's "quoted"]]`},
		{`print x=strlen('Hello\u00A0World'),y=substring('a\u0000b',1),z=toupper('a\u0000é')`, "[[11 \x00b A\x00É]]"},
		{`print x=replace_strings('a\u0000b',dynamic(['\u0000']),dynamic(['X']))`, `[[aXb]]`},
		{`print x=strlen("Hello"', '@"world!"),y=strlen("Hello" ', ' @"world!")`, `[[13 13]]`},
		{"print x=strlen(\"Hello\" // comment\n', '@\"world!\")", `[[13]]`},
		{`print x=@'C:\Folder\it''s',y=h'visible',z=H@"say ""hi"""`, `[[C:\Folder\it's visible say "hi"]]`},
		{"print x=```first\nsecond\\n```", "[[first\nsecond\\n]]"},
		{`print x='\U0001F600\uD83D\uDE00'`, `[[😀😀]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
	for _, query := range []string{`print x='\z'`, `print x='\x1'`, `print x='\u00q0'`, `print x='\UFFFFFFFF'`} {
		if _, err := Compile(query, time.Now()); err == nil {
			t.Errorf("accepted malformed string %s", query)
		}
	}
	for _, query := range []string{`Events | search 'Power' 'Shell'`, `Events | search 'Power\x53hell'`} {
		for _, term := range leadingSearchTerms(query) {
			if term != `"PowerShell"` {
				t.Errorf("search term %q", term)
			}
		}
		if len(leadingSearchTerms(query)) != 1 {
			t.Errorf("missing full-text term: %s", query)
		}
	}
}
