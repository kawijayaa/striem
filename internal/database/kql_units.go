package database

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"
	_ "time/tzdata" // datetime_utc_to_local needs the IANA database in minimal images.

	"github.com/kawijayaa/ksql/runtime"
	"github.com/kawijayaa/striem/internal/eventtime"
)

// Unit factors follow UnitsNet, which Kusto's convert_* functions use: each
// unit's size in the quantity's base unit (degree, joule, newton, meter,
// kilogram, meter per second, cubic meter).
var unitFactors = map[string]map[string]float64{
	"angle": {
		"arcminute": 1.0 / 60, "arcsecond": 1.0 / 3600, "centiradian": 0.01 * 180 / math.Pi,
		"deciradian": 0.1 * 180 / math.Pi, "degree": 1, "gradian": 0.9, "microdegree": 1e-6,
		"microradian": 1e-6 * 180 / math.Pi, "millidegree": 1e-3, "milliradian": 1e-3 * 180 / math.Pi,
		"nanodegree": 1e-9, "nanoradian": 1e-9 * 180 / math.Pi, "natomil": 360.0 / 6400,
		"radian": 180 / math.Pi, "revolution": 360,
	},
	"energy": {
		"britishthermalunit": 1055.05585262, "calorie": 4.184, "decathermec": 1.05505585262e9,
		"decathermimperial": 1.05505585257348e9, "decathermus": 1.054804e9, "electronvolt": 1.602176634e-19,
		"erg": 1e-7, "footpound": 1.355817948, "gigabritishthermalunit": 1055.05585262e9,
		"gigaelectronvolt": 1.602176634e-10, "gigajoule": 1e9, "gigawattday": 86400e9, "gigawatthour": 3600e9,
		"horsepowerhour": 2.6845195377e6, "joule": 1, "kilobritishthermalunit": 1055.05585262e3,
		"kilocalorie": 4184, "kiloelectronvolt": 1.602176634e-16, "kilojoule": 1e3, "kilowattday": 86400e3,
		"kilowatthour": 3600e3, "megabritishthermalunit": 1055.05585262e6, "megacalorie": 4.184e6,
		"megaelectronvolt": 1.602176634e-13, "megajoule": 1e6, "megawattday": 86400e6, "megawatthour": 3600e6,
		"millijoule": 1e-3, "teraelectronvolt": 1.602176634e-7, "terawattday": 86400e12, "terawatthour": 3600e12,
		"thermec": 1.05505585262e8, "thermimperial": 1.05505585257348e8, "thermus": 1.054804e8,
		"wattday": 86400, "watthour": 3600,
	},
	"force": {
		"decanewton": 10, "dyn": 1e-5, "kilogramforce": 9.80665, "kilonewton": 1e3, "kilopond": 9.80665,
		"kilopoundforce": 4448.2216152605, "meganewton": 1e6, "micronewton": 1e-6, "millinewton": 1e-3,
		"newton": 1, "ounceforce": 0.27801385095378125, "poundal": 0.138254954376, "poundforce": 4.4482216152605,
		"shorttonforce": 8896.443230521, "tonneforce": 9806.65,
	},
	"length": {
		"angstrom": 1e-10, "astronomicalunit": 1.4959787070e11, "centimeter": 1e-2, "chain": 20.1168,
		"datamile": 1828.8, "decameter": 10, "decimeter": 0.1, "dtppica": 0.0254 / 6, "dtppoint": 0.0254 / 72,
		"fathom": 1.8288, "foot": 0.3048, "hand": 0.1016, "hectometer": 100, "inch": 0.0254,
		"kilolightyear": 9.46073047258e18, "kilometer": 1e3, "kiloparsec": 3.08567758128e19,
		"lightyear": 9.46073047258e15, "megalightyear": 9.46073047258e21, "megaparsec": 3.08567758128e22,
		"meter": 1, "microinch": 2.54e-8, "micrometer": 1e-6, "mil": 2.54e-5, "mile": 1609.344,
		"millimeter": 1e-3, "nanometer": 1e-9, "nauticalmile": 1852, "parsec": 3.08567758128e16,
		"printerpica": 0.0254 / 6.0225, "printerpoint": 0.0254 / 72.27, "shackle": 27.432,
		"solarradius": 6.95510000e8, "twip": 0.0254 / 1440, "ussurveyfoot": 1200.0 / 3937, "yard": 0.9144,
	},
	"mass": {
		"centigram": 1e-5, "decagram": 1e-2, "decigram": 1e-4, "earthmass": 5.9722e24, "grain": 6.479891e-5,
		"gram": 1e-3, "hectogram": 0.1, "kilogram": 1, "kilopound": 453.59237, "kilotonne": 1e6,
		"longhundredweight": 50.80234544, "longton": 1016.0469088, "megapound": 453592.37, "megatonne": 1e9,
		"microgram": 1e-9, "milligram": 1e-6, "nanogram": 1e-12, "ounce": 0.028349523125, "pound": 0.45359237,
		"shorthundredweight": 45.359237, "shortton": 907.18474, "slug": 14.593902937206365,
		"solarmass": 1.98947e30, "stone": 6.35029318, "tonne": 1000,
	},
	"volume": {
		"acrefoot": 1 / 0.000810714, "autablespoon": 2e-5, "boardfoot": 0.002359737216,
		"centiliter": 1e-5, "cubiccentimeter": 1e-6, "cubicdecimeter": 1e-3, "cubicfoot": 0.028316846592,
		"cubichectometer": 1e6, "cubicinch": 1.6387064e-5, "cubickilometer": 1e9, "cubicmeter": 1,
		"cubicmicrometer": 1e-18, "cubicmile": 4.16818182544058e9, "cubicmillimeter": 1e-9,
		"cubicyard": 0.764554857984, "decaliter": 1e-2, "decausgallon": 0.03785411784, "deciliter": 1e-4,
		"deciusgallon": 0.0003785411784, "hectocubicfoot": 2.8316846592, "hectocubicmeter": 100,
		"hectoliter": 0.1, "hectousgallon": 0.3785411784, "imperialbeerbarrel": 0.16365924,
		"imperialgallon": 0.00454609, "imperialounce": 2.84130625e-5, "imperialpint": 5.6826125e-4,
		"kilocubicfoot": 28.316846592, "kilocubicmeter": 1e3, "kiloimperialgallon": 4.54609,
		"kiloliter": 1, "kilousgallon": 3.785411784, "liter": 1e-3, "megacubicfoot": 28316.846592,
		"megaimperialgallon": 4546.09, "megaliter": 1e3, "megausgallon": 3785.411784, "metriccup": 2.5e-4,
		"metricteaspoon": 5e-6, "microliter": 1e-9, "milliliter": 1e-6, "oilbarrel": 0.158987294928,
		"uktablespoon": 1.5e-5, "usbeerbarrel": 0.1173477658, "uscustomarycup": 2.365882365e-4,
		"usgallon": 0.003785411784, "uslegalcup": 2.4e-4, "usounce": 2.95735295625e-5,
		"uspint": 4.73176473e-4, "usquart": 9.46352946e-4, "ustablespoon": 1.478676478125e-5,
		"usteaspoon": 4.92892159375e-6,
	},
}

func init() {
	// Speed units combine a length and a time unit.
	speed := map[string]float64{"knot": 1852.0 / 3600}
	for name, meters := range map[string]float64{"centimeter": 1e-2, "decimeter": 0.1, "foot": 0.3048, "inch": 0.0254,
		"kilometer": 1e3, "meter": 1, "micrometer": 1e-6, "mile": 1609.344, "millimeter": 1e-3, "nanometer": 1e-9,
		"ussurveyfoot": 1200.0 / 3937, "yard": 0.9144} {
		for unit, seconds := range map[string]float64{"hour": 3600, "minute": 60, "second": 1} {
			speed[name+"per"+unit] = meters / seconds
		}
	}
	unitFactors["speed"] = speed
}

func kqlConvertUnit(quantity string, value, fromValue, toValue any) any {
	number, ok := geoNumber(value)
	from, okFrom := textArgument(fromValue)
	to, okTo := textArgument(toValue)
	if !ok || !okFrom || !okTo || sqliteNull(fromValue) || sqliteNull(toValue) {
		return nil
	}
	from, to = strings.ToLower(from), strings.ToLower(to)
	if quantity == "temperature" {
		kelvin, ok := toKelvin(number, from)
		if !ok {
			return nil
		}
		result, ok := fromKelvin(kelvin, to)
		if !ok {
			return nil
		}
		return finiteJSON(result)
	}
	if quantity == "angle" && (from == "tilt" || to == "tilt") {
		// UnitsNet's tilt is the sine of the angle.
		degrees := number
		if from == "tilt" {
			degrees = math.Asin(number) * 180 / math.Pi
		} else if factor, ok := unitFactors["angle"][from]; ok {
			degrees = number * factor
		} else {
			return nil
		}
		if to == "tilt" {
			return finiteJSON(math.Sin(degrees * math.Pi / 180))
		}
		factor, ok := unitFactors["angle"][to]
		if !ok {
			return nil
		}
		return finiteJSON(degrees / factor)
	}
	units := unitFactors[quantity]
	fromFactor, okFrom := units[from]
	toFactor, okTo := units[to]
	if !okFrom || !okTo {
		return nil
	}
	if from == to {
		return number
	}
	return finiteJSON(number * fromFactor / toFactor)
}

func toKelvin(value float64, unit string) (float64, bool) {
	switch unit {
	case "kelvin":
		return value, true
	case "degreecelsius":
		return value + 273.15, true
	case "millidegreecelsius":
		return value/1000 + 273.15, true
	case "degreefahrenheit":
		return (value + 459.67) * 5 / 9, true
	case "degreerankine":
		return value * 5 / 9, true
	case "degreedelisle":
		return 373.15 - value*2/3, true
	case "degreenewton":
		return value*100/33 + 273.15, true
	case "degreereaumur":
		return value*5/4 + 273.15, true
	case "degreeroemer":
		return (value-7.5)*40/21 + 273.15, true
	case "solartemperature":
		return value * 5778, true
	}
	return 0, false
}

func fromKelvin(kelvin float64, unit string) (float64, bool) {
	switch unit {
	case "kelvin":
		return kelvin, true
	case "degreecelsius":
		return kelvin - 273.15, true
	case "millidegreecelsius":
		return (kelvin - 273.15) * 1000, true
	case "degreefahrenheit":
		return kelvin*9/5 - 459.67, true
	case "degreerankine":
		return kelvin * 9 / 5, true
	case "degreedelisle":
		return (373.15 - kelvin) * 3 / 2, true
	case "degreenewton":
		return (kelvin - 273.15) * 33 / 100, true
	case "degreereaumur":
		return (kelvin - 273.15) * 4 / 5, true
	case "degreeroemer":
		return (kelvin-273.15)*21/40 + 7.5, true
	case "solartemperature":
		return kelvin / 5778, true
	}
	return 0, false
}

// kqlFormatBytes formats a size with 1024-based units, auto-selected (the
// largest unit not exceeding the size) unless one is given.
func kqlFormatBytes(sizeValue, precisionValue, unitsValue any) any {
	size, ok := geoNumber(sizeValue)
	precision, okPrecision := precisionValue.(int64)
	if !ok || !okPrecision || precision < 0 || precision > 15 {
		return nil
	}
	units := []string{"Bytes", "KB", "MB", "GB", "TB", "PB", "EB"}
	index := 0
	if text, ok := textArgument(unitsValue); ok && !sqliteNull(unitsValue) && text != "" {
		index = -1
		for i, unit := range units {
			if strings.EqualFold(unit, text) {
				index = i
			}
		}
		if index < 0 {
			return nil
		}
	} else {
		for index+1 < len(units) && math.Abs(size) >= math.Pow(1024, float64(index+1)) {
			index++
		}
	}
	scaled := size / math.Pow(1024, float64(index))
	return runtime.FormatReal(roundHalfAway(scaled, int(precision))) + " " + units[index]
}

func roundHalfAway(value float64, digits int) float64 {
	scale := math.Pow(10, float64(digits))
	return math.Round(value*scale) / scale
}

func kqlStringSize(value any) any {
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) {
		return nil
	}
	return int64(len(text))
}

// .NET serializes a Guid with its first three groups little-endian.
func guidBytes(text string) ([]byte, bool) {
	raw, err := hex.DecodeString(strings.ReplaceAll(text, "-", ""))
	if err != nil || len(raw) != 16 || len(text) != 36 {
		return nil, false
	}
	return []byte{raw[3], raw[2], raw[1], raw[0], raw[5], raw[4], raw[7], raw[6],
		raw[8], raw[9], raw[10], raw[11], raw[12], raw[13], raw[14], raw[15]}, true
}

func kqlBase64EncodeFromGUID(value any) any {
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) {
		return nil
	}
	raw, ok := guidBytes(strings.ToLower(text))
	if !ok {
		return nil
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func kqlBase64DecodeToGUID(value any) any {
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(text)
	if err != nil || len(raw) != 16 {
		return nil
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", []byte{raw[3], raw[2], raw[1], raw[0]}, []byte{raw[5], raw[4]},
		[]byte{raw[7], raw[6]}, raw[8:10], raw[10:16])
}

// kqlNewGUID returns a random (version 4) GUID.
func kqlNewGUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}

// Time zone names are IANA identifiers; an unknown zone yields null.
func timeZone(value any) (*time.Location, bool) {
	name, ok := textArgument(value)
	if !ok || sqliteNull(value) || name == "" || len(name) > 64 || strings.Contains(name, "..") {
		return nil, false
	}
	location, err := time.LoadLocation(name)
	return location, err == nil
}

func kqlDatetimeUTCToLocal(value, zoneValue any) any {
	when, ok := parsedDatetime(value)
	location, okZone := timeZone(zoneValue)
	if !ok || !okZone {
		return nil
	}
	local := when.In(location)
	wall := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), local.Second(), local.Nanosecond(), time.UTC)
	if wall.Year() < 1 || wall.Year() > 9999 {
		return nil
	}
	return eventtime.Format(wall)
}

func kqlDatetimeLocalToUTC(value, zoneValue any) any {
	wall, ok := parsedDatetime(value)
	location, okZone := timeZone(zoneValue)
	if !ok || !okZone {
		return nil
	}
	local := time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), wall.Nanosecond(), location)
	utc := local.UTC()
	if utc.Year() < 1 || utc.Year() > 9999 {
		return nil
	}
	return eventtime.Format(utc)
}

// Beta distribution functions; parameters outside the distribution's domain
// would be NaN in Kusto, which SQLite cannot store, so they return null.
func betaArguments(xValue, alphaValue, betaValue any) (float64, float64, float64, bool) {
	x, okX := geoNumber(xValue)
	alpha, okA := geoNumber(alphaValue)
	beta, okB := geoNumber(betaValue)
	return x, alpha, beta, okX && okA && okB && alpha > 0 && beta > 0
}

func kqlBetaPDF(xValue, alphaValue, betaValue any) any {
	x, alpha, beta, ok := betaArguments(xValue, alphaValue, betaValue)
	if !ok || x < 0 || x > 1 {
		return nil
	}
	la, _ := math.Lgamma(alpha)
	lb, _ := math.Lgamma(beta)
	lab, _ := math.Lgamma(alpha + beta)
	return finiteJSON(math.Exp((alpha-1)*math.Log(x) + (beta-1)*math.Log1p(-x) + lab - la - lb))
}

func kqlBetaCDF(xValue, alphaValue, betaValue any) any {
	x, alpha, beta, ok := betaArguments(xValue, alphaValue, betaValue)
	if !ok || x < 0 || x > 1 {
		return nil
	}
	return regularizedBeta(x, alpha, beta)
}

func kqlBetaInv(pValue, alphaValue, betaValue any) any {
	p, alpha, beta, ok := betaArguments(pValue, alphaValue, betaValue)
	if !ok || p < 0 || p > 1 {
		return nil
	}
	low, high := 0.0, 1.0
	for i := 0; i < 200; i++ {
		mid := (low + high) / 2
		if regularizedBeta(mid, alpha, beta) < p {
			low = mid
		} else {
			high = mid
		}
	}
	return (low + high) / 2
}

// regularizedBeta is I_x(a, b) by Lentz's continued fraction.
func regularizedBeta(x, a, b float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	if x > (a+1)/(a+b+2) {
		return 1 - regularizedBeta(1-x, b, a)
	}
	la, _ := math.Lgamma(a)
	lb, _ := math.Lgamma(b)
	lab, _ := math.Lgamma(a + b)
	front := math.Exp(a*math.Log(x)+b*math.Log1p(-x)+lab-la-lb) / a
	const tiny = 1e-300
	f, c, d := 1.0, 1.0, 0.0
	for i := 0; i <= 300; i++ {
		m := float64(i / 2)
		var numerator float64
		switch {
		case i == 0:
			numerator = 1
		case i%2 == 0:
			numerator = m * (b - m) * x / ((a + 2*m - 1) * (a + 2*m))
		default:
			numerator = -(a + m) * (a + b + m) * x / ((a + 2*m) * (a + 2*m + 1))
		}
		d = 1 + numerator*d
		if math.Abs(d) < tiny {
			d = tiny
		}
		d = 1 / d
		c = 1 + numerator/c
		if math.Abs(c) < tiny {
			c = tiny
		}
		f *= c * d
		if math.Abs(1-c*d) < 1e-15 {
			break
		}
	}
	return front * (f - 1)
}
