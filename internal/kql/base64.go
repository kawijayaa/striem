package kql

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

func base64Options() []ksql.Option {
	var options []ksql.Option
	for _, config := range []struct {
		name, runtime string
		input, output ksql.ScalarType
	}{
		{"url_encode", "kql_url_encode", ksql.TypeString, ksql.TypeString},
		{"url_encode_component", "kql_url_encode_component", ksql.TypeString, ksql.TypeString},
		{"url_decode", "kql_url_decode", ksql.TypeString, ksql.TypeString},
		{"gzip_compress_to_base64_string", "kql_gzip_compress", ksql.TypeString, ksql.TypeString},
		{"gzip_decompress_from_base64_string", "kql_gzip_decompress", ksql.TypeString, ksql.TypeString},
		{"zlib_compress_to_base64_string", "kql_zlib_compress", ksql.TypeString, ksql.TypeString},
		{"zlib_decompress_from_base64_string", "kql_zlib_decompress", ksql.TypeString, ksql.TypeString},
		{"base64_encode_tostring", "kql_base64_encode_tostring", ksql.TypeString, ksql.TypeString},
		{"base64_encodestring", "kql_base64_encode_tostring", ksql.TypeString, ksql.TypeString},
		{"base64_decode_tostring", "kql_base64_decode_tostring", ksql.TypeString, ksql.TypeString},
		{"base64_decodestring", "kql_base64_decode_tostring", ksql.TypeString, ksql.TypeString},
		{"base64_decode_toarray", "kql_base64_decode_toarray", ksql.TypeString, ksql.TypeDynamic},
		{"base64_encode_fromarray", "kql_base64_encode_fromarray", ksql.TypeDynamic, ksql.TypeString},
	} {
		config := config
		options = append(options, ksql.WithTypedFunction(config.name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 1 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires 1 argument", config.name)
			}
			if args[0].Type != ksql.TypeUnknown && args[0].Type != config.input {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires %s input", config.name, config.input)
			}
			return ksql.TypedExpression{Type: config.output, SQL: &sqlast.Call{Name: config.runtime, Args: []sqlast.Expr{args[0].SQL}}}, nil
		}))
	}
	return options
}
