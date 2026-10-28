package database

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // datetime_utc_to_local needs the IANA database in minimal images.

	"github.com/kawijayaa/striem/internal/eventtime"
)

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
