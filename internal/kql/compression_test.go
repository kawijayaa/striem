package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestCompressionExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, codec := range []string{"gzip", "zlib"} {
		for _, tc := range []struct{ query, want string }{
			{fmt.Sprintf(`print x=%s_decompress_from_base64_string(%s_compress_to_base64_string("helloé😀"))`, codec, codec), `[[helloé😀]]`},
			{fmt.Sprintf(`datatable(s:string)["abc","","é"]|project x=%s_decompress_from_base64_string(%s_compress_to_base64_string(s))`, codec, codec), `[[abc] [] [é]]`},
			{fmt.Sprintf(`print x=%s_decompress_from_base64_string("x0x0x0")`, codec), `[[<nil>]]`},
			{fmt.Sprintf(`print x=%s_compress_to_base64_string("abc")|getschema|project ColumnType`, codec), `[[string]]`},
		} {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
			}
		}
		for _, fn := range []string{codec + "_compress_to_base64_string", codec + "_decompress_from_base64_string"} {
			for _, args := range []string{"", "1", `"a","b"`} {
				if _, err := Compile("print x="+fn+"("+args+")", time.Now()); err == nil {
					t.Fatalf("invalid signature accepted %s(%s)", fn, args)
				}
			}
		}
	}
}
