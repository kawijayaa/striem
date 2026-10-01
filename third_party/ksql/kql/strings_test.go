package kql

import "testing"

func TestDecodeStrings(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`'hello\nworld'`, "hello\nworld"},
		{`"hello\nworld"`, "hello\nworld"},
		{`'\a\b\f\n\r\t\v'`, "\a\b\f\n\r\t\v"},
		{`'it\'s "quoted"'`, `it's "quoted"`},
		{`"it\'s \"quoted\""`, `it's "quoted"`},
		{`'\u00A0\U0001F600\uD83D\uDE00'`, "\u00a0😀😀"},
		{`'\x41\101\0\7\77\400'`, "AA\x00\a? 0"},
		{`@'C:\Folder\it''s'`, `C:\Folder\it's`},
		{`h'hello\tthere'`, "hello\tthere"},
		{`H@"say ""hi""\n"`, `say "hi"\n`},
		{"```a\nb\\n```", "a\nb\\n"},
	} {
		if got, err := DecodeString(tc.input); err != nil || got != tc.want {
			t.Errorf("%s: got %q error=%v want %q", tc.input, got, err, tc.want)
		}
	}
	for _, input := range []string{`'\z'`, `'\x1'`, `'\u00q0'`, `'\UFFFFFFFF'`, `'\uD800'`, `'\uDC00'`} {
		_, errors := Lex("print x=" + input)
		if len(errors) == 0 {
			t.Errorf("accepted malformed escape %s", input)
		}
	}
}
