package database

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/ua-parser/uap-go/uaparser"
)

// parse_user_agent runs the uap-core patterns Kusto's implementation is
// built on, through uap-go's RE2 regexes, so matching time is linear in the
// input. Inputs over maxUserAgentBytes return null to bound the work.
const maxUserAgentBytes = 8 << 10

var userAgentParser = sync.OnceValue(func() *uaparser.Parser {
	parser, err := uaparser.New(uaparser.WithCacheSize(4096))
	if err != nil {
		panic(err)
	}
	return parser
})

type orderedField struct{ key, value string }

func writeObject(builder *strings.Builder, fields []orderedField) {
	builder.WriteByte('{')
	for i, field := range fields {
		if i > 0 {
			builder.WriteByte(',')
		}
		key, _ := json.Marshal(field.key)
		value, _ := json.Marshal(field.value)
		builder.Write(key)
		builder.WriteByte(':')
		builder.Write(value)
	}
	builder.WriteByte('}')
}

// userAgentTargets reads look-for: one target name, or a JSON array of them.
func userAgentTargets(lookFor string) (map[string]bool, error) {
	names := []string{lookFor}
	if trimmed := strings.TrimSpace(lookFor); strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal([]byte(trimmed), &names); err != nil {
			return nil, fmt.Errorf("parse_user_agent look-for must be a string or an array of strings")
		}
	} else if strings.HasPrefix(trimmed, `"`) {
		var name string
		if json.Unmarshal([]byte(trimmed), &name) == nil {
			names = []string{name}
		}
	}
	targets := map[string]bool{}
	for _, name := range names {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "browser", "os", "device":
			targets[strings.ToLower(strings.TrimSpace(name))] = true
		default:
			return nil, fmt.Errorf("parse_user_agent look-for must be browser, os or device, not %q", name)
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("parse_user_agent requires a look-for target")
	}
	return targets, nil
}

func kqlParseUserAgent(value, lookForValue any) (any, error) {
	lookFor, ok := textArgument(lookForValue)
	if !ok || sqliteNull(lookForValue) {
		return nil, fmt.Errorf("parse_user_agent requires a look-for target")
	}
	targets, err := userAgentTargets(lookFor)
	if err != nil {
		return nil, err
	}
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) || len(text) > maxUserAgentBytes {
		return nil, nil
	}
	parser := userAgentParser()
	var builder strings.Builder
	builder.WriteByte('{')
	first := true
	section := func(name string, fields []orderedField) {
		if !first {
			builder.WriteByte(',')
		}
		first = false
		builder.WriteString(`"` + name + `":`)
		writeObject(&builder, fields)
	}
	if targets["browser"] {
		agent := parser.ParseUserAgent(text)
		section("Browser", []orderedField{{"Family", agent.Family}, {"MajorVersion", agent.Major}, {"MinorVersion", agent.Minor}, {"Patch", agent.Patch}})
	}
	if targets["os"] {
		os := parser.ParseOs(text)
		section("OperatingSystem", []orderedField{{"Family", os.Family}, {"MajorVersion", os.Major}, {"MinorVersion", os.Minor}, {"Patch", os.Patch}, {"PatchMinor", os.PatchMinor}})
	}
	if targets["device"] {
		device := parser.ParseDevice(text)
		section("Device", []orderedField{{"Family", device.Family}, {"Brand", device.Brand}, {"Model", device.Model}})
	}
	builder.WriteByte('}')
	return builder.String(), nil
}
