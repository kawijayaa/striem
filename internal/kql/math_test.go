package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestMathAndPredicateFunctions(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		// Microsoft examples: log2(5), max_of numbers, null precedence and datetimes.
		{`print result=log2(5)`, `[[2.321928094887362]]`},
		{`print result = max_of(10, 1, -3, 17), low = min_of(10, 1, -3, 17)`, `[[17 -3]]`},
		{`datatable (A: int, B: int)[1, 6, 8, 1, int(null), 2, 1, int(null), int(null), int(null)] | project r=max_of(A, B), m=min_of(A, B)`, `[[6 1] [8 1] [2 2] [1 1] [<nil> <nil>]]`},
		{`datatable (A: datetime, B: datetime)[datetime(2024-12-15 07:15:22), datetime(2024-12-15 07:15:24), datetime(2024-12-15 10:45:00), datetime(2024-12-14 10:45:00)] | project maxDate = max_of(A, B)`, `[[2024-12-15T07:15:24.000000000Z] [2024-12-15T10:45:00.000000000Z]]`},
		{`print a=max_of("b", "a", "c"), b=max_of(1, 2.5), c=min_of(int(1), long(-2))`, `[[c 2.5 -2]]`},
		{`print p=pi(), d=degrees(pi()), r=radians(180), c=round(cot(pi()/4), 9), e=exp2(10), t=exp10(3), g=gamma(5), lg=round(loggamma(5), 9), erf=round(erf(0.5), 9), erfc=round(erfc(0.5), 9)`,
			`[[3.141592653589793 180 3.141592653589793 1 1024 1000 24 3.17805383 0.520499878 0.479500122]]`},
		{`print neg=log2(-1), zero=log2(0), big=exp2(5000)`, `[[<nil> <nil> <nil>]]`},
		{`print f=isfinite(1.5), i=isinf(1.5), n=isnan(1.5), nf=isfinite(real(null)), ni=isinf(real(null)), nn=isnan(real(null))`, `[[1 0 0 <nil> 0 0]]`},
		{`print a=strcmp("abc","abd"), b=strcmp("abc","abc"), c=strcmp("b","a"), d=strcmp("","")`, `[[-1 0 1 0]]`},
		{`print a=isascii("abc"), b=isascii("é"), c=isutf8("é"), d=notempty(""), e=notempty("x"), f=notnull(int(null)), g=weekofyear(datetime(2015-12-14))`, `[[1 0 1 0 1 0 51]]`},
		{`print n=max_of(1, 2) | getschema | project ColumnType`, `[[long]]`},
		// Microsoft's round examples, half away from zero on the decimal value.
		{`print a=round(2.98765, 3), b=round(2.15, 1), c=round(2.15), d=round(-50.55, -2), e=round(21.5, -1)`, `[[2.988 2.2 2 -100 20]]`},
		{`print a=round(-2.5), b=round(2.5), c=round(1.005, 2), d=round(1234, -2), e=round(1250, -2), f=round(real(null)), g=round(5, 3), h=round(1e300, -299), i=round(2.5, 999)`, `[[-3 3 1.01 1200 1300 <nil> 5 1e+300 <nil>]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	for _, query := range []string{`print max_of(1)`, `print max_of(1, "a")`, `print strcmp(1, "a")`, `print log2("x")`, `print pi(1)`, `print isascii(1)`} {
		if _, err := Compile(query, time.Now()); err == nil {
			t.Fatalf("invalid call accepted: %s", query)
		}
	}
}
