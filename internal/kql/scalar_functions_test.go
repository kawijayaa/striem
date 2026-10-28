package kql

import (
	"fmt"
	"testing"
	"time"
)

// Microsoft's examples for unit conversion, byte formatting, guid base64,
// time zones and the beta distribution, with invalid-input edges.
func TestScalarRuntimeFunctions(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print a=convert_angle(1.2, 'Degree', 'Arcminute'), e=round(convert_energy(1.2, 'Joule', 'BritishThermalUnit'), 15), f=convert_force(1.2, 'Newton', 'Decanewton'), l=round(convert_length(1.2, 'Meter', 'Foot'), 12), m=round(convert_mass(1.2, 'Kilogram', 'Pound'), 12), s=round(convert_speed(1.2, 'MeterPerSecond', 'CentimeterPerHour'), 6), t=round(convert_temperature(1.2, 'Kelvin', 'DegreeCelsius'), 9), v=round(convert_volume(1.2, 'CubicMeter', 'AcreFoot'), 10)`,
			`[[72 0.001137380544376 0.12 3.937007874016 2.645547146219 432000 -271.95 0.0009728568]]`},
		{`print f=round(convert_temperature(100, 'DegreeCelsius', 'DegreeFahrenheit'), 9), same=convert_length(5, 'meter', 'METER'), tilt=round(convert_angle(30, 'Degree', 'Tilt'), 12), bad=convert_length(1, 'Meter', 'Parsnip'), n=convert_mass(real(null), 'Gram', 'Kilogram')`, `[[212 5 0.5 <nil> <nil>]]`},
		{`print v1 = format_bytes(564), v2 = format_bytes(10332, 1), v3 = format_bytes(20010332), v4 = format_bytes(20010332, 2), v5 = format_bytes(20010332, 0, "KB"), bad=format_bytes(1, 0, "XB")`, `[[564 Bytes 10.1 KB 19 MB 19.08 MB 19541 KB <nil>]]`},
		{`print a=string_size("hello"), b=string_size("⒦⒰⒮⒯⒪"), c=string_size(""), e=base64_encode_fromguid(toguid("ae3133f2-6e22-49ae-b06a-16e6a9b212eb")), d=base64_decode_toguid("JpbpECu8dUy7Pv5gbeJXAA=="), bad=base64_decode_toguid("AAAA")`,
			`[[5 15 0 8jMxriJurkmwahbmqbIS6w== 10e99626-bc2b-4c75-bb3e-fe606de25700 <nil>]]`},
		// Decoding inverts encoding (.NET Guid byte order).
		{`print r=base64_decode_toguid(base64_encode_fromguid(toguid("ae3133f2-6e22-49ae-b06a-16e6a9b212eb")))`, `[[ae3133f2-6e22-49ae-b06a-16e6a9b212eb]]`},
		{`datatable(local_dt: datetime, tz: string)[datetime(2020-02-02 20:02:20), 'US/Pacific', datetime(2020-02-02 20:02:20), 'Europe/Paris', datetime(2020-07-01 12:00), 'Nowhere/City'] | extend utc_dt = datetime_local_to_utc(local_dt, tz), back = datetime_utc_to_local(datetime_local_to_utc(local_dt, tz), tz) | project utc_dt, back`,
			`[[2020-02-03T04:02:20.000000000Z 2020-02-02T20:02:20.000000000Z] [2020-02-02T19:02:20.000000000Z 2020-02-02T20:02:20.000000000Z] [<nil> <nil>]]`},
		{`print c=round(beta_cdf(0.9, 10, 20), 12), p=round(beta_pdf(0.5, 10, 20), 12), i=round(beta_inv(0.1, 10, 20), 12), out=beta_cdf(1.5, 10, 20), neg=beta_pdf(0.1, -1, 20), high=beta_inv(1.5, 10, 20)`,
			`[[1 0.746176019311 0.226415022389 <nil> <nil> <nil>]]`},
		{`range i from 1 to 50 step 1 | extend g=new_guid() | summarize distinct=dcount(g), shaped=countif(g matches regex "^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")`, `[[50 50]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	for _, query := range []string{`print convert_length("a", "Meter", "Foot")`, `print new_guid(1)`, `print datetime_utc_to_local(1, "UTC")`} {
		if _, err := Compile(query, time.Now()); err == nil {
			t.Fatalf("invalid call accepted: %s", query)
		}
	}
}

// Microsoft's covariance examples, plus single-row and null-pair edges.
func TestCovarianceAggregates(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`datatable(x:real, y:real) [1.0, 14.0, 2.0, 10.0, 3.0, 17.0, 4.0, 20.0, 5.0, 50.0] | summarize s=covariance(x, y), p=covariancep(x, y)`, `[[20.5 16.4]]`},
		{`range x from 1 to 100 step 1 | extend y = iff(x % 2 == 0, x * 2, x * 3) | summarize s=covarianceif(x, y, x % 3 == 0), p=round(covariancepif(x, y, x % 3 == 0), 9)`, `[[2142 2077.090909091]]`},
		{`datatable(x:real, y:real) [1.0, 2.0, real(null), 5.0, 3.0, real(null)] | summarize one=covariance(x, y), pop=covariancep(x, y)`, `[[<nil> 0]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	if _, err := Compile(`print x=1 | summarize covariance(x, "a")`, time.Now()); err == nil {
		t.Fatal("non-numeric covariance accepted")
	}
}

// Microsoft's pack_all example, which packs every input column.
func TestPackAll(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`datatable(Source:string,Target:string,CharsCount:long)['555-1234','555-1212',46, '555-1313','',42, '','555-3456',74] | extend Packed=pack_all(), PackedIgnoreNullEmpty=pack_all(true) | project Packed, PackedIgnoreNullEmpty`,
			`[[{"CharsCount":46,"Source":"555-1234","Target":"555-1212"} {"CharsCount":46,"Source":"555-1234","Target":"555-1212"}] [{"CharsCount":42,"Source":"555-1313","Target":""} {"CharsCount":42,"Source":"555-1313"}] [{"CharsCount":74,"Source":"","Target":"555-3456"} {"CharsCount":74,"Target":"555-3456"}]]`},
		{`datatable(a:long, b:dynamic)[1, dynamic(null), 2, dynamic([1])] | project p=pack_all(true)`, `[[{"a":1}] [{"a":2,"b":[1]}]]`},
		{`print x=1 | project p=pack_all(false)`, `[[{"x":1}]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	if _, err := Compile(`print x=1 | project pack_all(x)`, time.Now()); err == nil {
		t.Fatal("non-constant pack_all argument accepted")
	}
}

// Microsoft's has_ipv4 examples and the prefix/any forms; temporal range.
func TestIPv4SearchAndTemporalRange(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print a=has_ipv4('05:04:54 127.0.0.1 GET /favicon.ico 404', '127.0.0.1'), b=has_ipv4('05:04:54 127.0.0.256 GET /favicon.ico 404', '127.0.0.256'), c=has_ipv4('05:04:54127.0.0.1 GET /favicon.ico 404', '127.0.0.1'), d=has_ipv4('ip=10.0.0.1.', '10.0.0.1'), e=has_ipv4('10.0.0.12', '10.0.0.1')`, `[[1 0 0 1 0]]`},
		{`print a=has_ipv4_prefix('05:04:54 127.0.0.1 GET', '127.0.'), b=has_ipv4_prefix('05:04:54 127.0.0.1 GET', '127.0'), c=has_ipv4_prefix('x 127.0.0.1', '127.0.0.1'), d=has_ipv4_prefix('x 127.0.0.1', '27.')`, `[[1 0 1 0]]`},
		{`print a=has_any_ipv4('src 10.0.0.1 dst 10.0.0.2', '1.1.1.1', '10.0.0.2'), b=has_any_ipv4('src 10.0.0.1', dynamic(['8.8.8.8', '10.0.0.1'])), c=has_any_ipv4_prefix('src 192.168.1.5', dynamic(['10.', '192.168.'])), d=has_any_ipv4('none', '1.1.1.1')`, `[[1 1 1 0]]`},
		{`print r=range(datetime(2024-01-01), datetime(2024-01-01 02:00), 1h), s=range(1h, 3h, 1h), e=range(datetime(2024-01-02), datetime(2024-01-01), 1h)`,
			`[[["2024-01-01T00:00:00.000000000Z","2024-01-01T01:00:00.000000000Z","2024-01-01T02:00:00.000000000Z"] ["01:00:00","02:00:00","03:00:00"] []]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	for _, query := range []string{`print range(datetime(2024-01-01), datetime(2024-01-02))`, `print range(datetime(2024-01-01), 5, 1h)`, `print has_any_ipv4(dynamic([]), '1.1.1.1')`} {
		if _, err := Compile(query, time.Now()); err == nil {
			t.Fatalf("invalid call accepted: %s", query)
		}
	}
}
