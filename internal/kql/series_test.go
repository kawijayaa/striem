package kql

import (
	"fmt"
	"testing"
	"time"
)

// Microsoft's series_* examples plus null, scalar and malformed-input edges.
func TestSeriesFunctions(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print a=series_add(dynamic([1,2,3]), dynamic([4,5,6])), b=series_subtract(dynamic([1,2,3]), 1), c=series_multiply(2, dynamic([1.5,2])), d=series_divide(dynamic([1,2]), dynamic([2,0])), e=series_pow(dynamic([2,3]), 2)`, `[[[5,7,9] [0,1,2] [3,4] [0.5,null] [4,9]]]`},
		{`print a=series_greater(dynamic([1,2,3]), 2), b=series_equals(dynamic([1,"x",3]), dynamic([1,2])), c=series_not_equals(dynamic([1,2]), 1)`, `[[[false,false,true] [true,null,null] [false,true]]]`},
		{`print a=series_abs(dynamic([-1,2.5,null])), b=series_sign(dynamic([-3,0,4])), c=series_floor(dynamic([1.5,-1.5])), d=series_ceiling(dynamic([1.2])), e=series_sqrt(dynamic([4,-1])), f=series_exp(dynamic([0]))`, `[[[1,2.5,null] [-1,0,1] [1,-2] [2] [2,null] [1]]]`},
		{`let data = datatable(arr: dynamic)[dynamic([null, 111.0, null, 36.0, 41.0, null, null, 16.0, 61.0, 33.0, null, null]), dynamic([null, 111, null, 36, 41, null, null, 16, 61, 33, null, null]), dynamic([null, null, null, null])]; data | project without_args = series_fill_linear(arr), wo_edges = series_fill_linear(arr, double(null), false), with_const = series_fill_linear(arr, double(null), true, 3.14159)`, `[[[111,111,73.5,36,41,32.66666666666667,24.333333333333336,16,61,33,33,33] [null,111,73.5,36,41,32.66666666666667,24.333333333333336,16,61,33,null,null] [111,111,73.5,36,41,32.66666666666667,24.333333333333336,16,61,33,33,33]] [[111,111,73,36,41,32,24,16,61,33,33,33] [null,111,73,36,41,32,24,16,61,33,null,null] [111,111,73,36,41,32,24,16,61,33,33,33]] [[0,0,0,0] [0,0,0,0] [3.14159,3.14159,3.14159,3.14159]]]`},
		{`print f=series_fill_forward(dynamic([null, null, 36, 41, null, null, 16, 61, 33, null, null])), b=series_fill_backward(dynamic([null, null, 36, 41, null, null, 16, 61, 33, null, null])), c=series_fill_const(dynamic([null, 2, -1]), 0, -1)`, `[[[null,null,36,41,41,41,16,61,33,33,33] [36,36,36,41,16,16,16,61,33,null,null] [null,2,0]]]`},
		{`print x=dynamic([23, 46, 23, 87, 4, 8, 3, 75, 2, 56, 13, 75, 32, 16, 29]) | project stats=series_stats_dynamic(x)`, `[[{"avg":32.8,"len":15,"max":87,"max_idx":3,"min":2,"min_idx":8,"stdev":28.503633853548266,"sum":492,"variance":812.4571428571428}]]`},
		{`print y=dynamic([2,5,6,8,11,15,17,18,25,26,31,30]) | extend fit=series_fit_line_dynamic(y) | project slope=round(todouble(fit.slope), 6), rsq=round(todouble(fit.rsquare), 6)`, `[[2.762238 0.979722]]`},
		{`print x=dynamic([30,28,5,27,31,38,29,80,25,37,30]) | project o=series_outliers(x)`, `[[[0,0,-1.5384615384615385,0,0,0,0,3.230769230769231,0,0,0]]]`},
		{`print x=range(1,5,1) | project avg=series_fir(x, dynamic([1,1,1]), true, true), sum=series_fir(x, dynamic([1,1]), false, false)`, `[[[1,2,3,4,3] [1,3,5,7,9]]]`},
		{"let x = range(1.0, 5, 1);\nprint x=x, y = series_iir(x, dynamic([1]), dynamic([1,-1]))\n| mv-expand x, y", `[[1 1] [2 3] [3 6] [4 10] [5 15]]`},
		{`print x=range(1,5,1) | project d=series_fir(x, dynamic([1,-1])), n=series_fir(x, dynamic([1,-1]), true)`, `[[[1,1,1,1,1] <nil>]]`},
		{`print m=series_magnitude(dynamic([1,2,3,4])), p=series_product(dynamic([1,2,3])), d=series_dot_product(dynamic([1,2,3]), dynamic([4,5,6])), c=round(series_cosine_similarity(dynamic([1,2,3]), dynamic([4,5,6])), 6), r=round(series_pearson_correlation(dynamic([1,2,3]), dynamic([2,4,7])), 6)`, `[[5.477225575051661 6 32 0.974632 0.993399]]`},
		{`print a=series_add(dynamic("x"), 1), b=series_fill_linear(dynamic(null)), c=series_stats_dynamic(dynamic([]))`, `[[<nil> <nil> {"avg":null,"len":0,"max":null,"max_idx":null,"min":null,"min_idx":null,"stdev":null,"sum":null,"variance":null}]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	for _, query := range []string{`print series_add(dynamic([1]))`, `print series_abs("x")`, `print series_iir(dynamic([1]))`, `print series_fill_linear()`} {
		if _, err := Compile(query, time.Now()); err == nil {
			t.Fatalf("invalid call accepted: %s", query)
		}
	}
}
