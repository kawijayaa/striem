package database

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

func urlInput(value any) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("URL parsing requires string input")
	}
	if len(text) > maxDecodeInputBytes {
		return "", fmt.Errorf("URL parsing input exceeds %d bytes", maxDecodeInputBytes)
	}
	if !utf8.ValidString(text) {
		return "", fmt.Errorf("URL parsing input must be valid UTF-8")
	}
	return text, nil
}

func urlParameters(query string) (map[string]string, error) {
	values, err := url.ParseQuery(query)
	if err != nil {
		return nil, err
	}
	parameters := make(map[string]string, len(values))
	for key, entries := range values {
		if !utf8.ValidString(key) {
			return nil, fmt.Errorf("invalid UTF-8 in query key")
		}
		for _, entry := range entries {
			if !utf8.ValidString(entry) {
				return nil, fmt.Errorf("invalid UTF-8 in query value")
			}
		}
		parameters[key] = entries[len(entries)-1]
	}
	return parameters, nil
}

func kqlParseURLQuery(value any) (any, error) {
	if sqliteNull(value) {
		return nil, nil
	}
	text, err := urlInput(value)
	if err != nil {
		return nil, err
	}
	parameters, err := urlParameters(text)
	if err != nil {
		return nil, nil
	}
	encoded, err := json.Marshal(map[string]any{"Query Parameters": parameters})
	return string(encoded), err
}

func kqlParseURL(value any) (any, error) {
	if sqliteNull(value) {
		return nil, nil
	}
	text, err := urlInput(value)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(text)
	if err != nil || !parsed.IsAbs() {
		return nil, nil
	}
	parameters, err := urlParameters(parsed.RawQuery)
	if err != nil {
		return nil, nil
	}
	username, password := "", ""
	if parsed.User != nil {
		username = parsed.User.Username()
		password, _ = parsed.User.Password()
	}
	for _, part := range []string{parsed.Path, parsed.Fragment, username, password} {
		if !utf8.ValidString(part) {
			return nil, nil
		}
	}
	encoded, err := json.Marshal(map[string]any{
		"Scheme": parsed.Scheme, "Host": parsed.Hostname(), "Port": parsed.Port(),
		"Path": strings.TrimPrefix(parsed.Path, "/"), "Username": username,
		"Password": password, "Query Parameters": parameters, "Fragment": parsed.Fragment,
	})
	return string(encoded), err
}
