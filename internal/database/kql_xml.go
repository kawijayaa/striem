package database

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const maxXMLDepth = 256

type xmlElement struct {
	name       xml.Name
	properties map[string]any
	counts     map[string]int
	text       strings.Builder
}

func xmlQualifiedName(name xml.Name) string {
	if name.Space != "" {
		return name.Space + ":" + name.Local
	}
	return name.Local
}

func (element *xmlElement) add(name string, value any) {
	count := element.counts[name]
	if count == 0 {
		element.properties[name] = value
	} else if count == 1 {
		element.properties[name] = []any{element.properties[name], value}
	} else {
		element.properties[name] = append(element.properties[name].([]any), value)
	}
	element.counts[name]++
}

func (element *xmlElement) value() any {
	text := strings.TrimSpace(element.text.String())
	if len(element.properties) == 0 {
		if text == "" {
			return nil
		}
		return text
	}
	if text != "" {
		element.properties["#text"] = text
	}
	return element.properties
}

func kqlParseXML(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	text, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("parse_xml requires string input")
	}
	if len(text) > maxDynamicInputBytes || !utf8.ValidString(text) {
		return nil, nil
	}
	text = strings.TrimPrefix(text, "\ufeff")
	// RawToken retains namespace prefixes for JSON property names. It does not
	// check matching tags, so the explicit stack below validates them.
	// No Entity map or external resolver is installed: DTD entities are never expanded.
	decoder := xml.NewDecoder(strings.NewReader(text))
	stack := []*xmlElement{}
	var root map[string]any
	for {
		before := decoder.InputOffset()
		token, err := decoder.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil
		}
		switch token := token.(type) {
		case xml.StartElement:
			if len(stack) == 0 && root != nil {
				return nil, nil
			}
			if len(stack) >= maxXMLDepth {
				return nil, fmt.Errorf("parse_xml nesting exceeds %d elements", maxXMLDepth)
			}
			element := &xmlElement{name: token.Name, properties: map[string]any{}, counts: map[string]int{}}
			for _, attribute := range token.Attr {
				name := "@" + xmlQualifiedName(attribute.Name)
				if _, duplicate := element.properties[name]; duplicate {
					return nil, nil
				}
				element.properties[name] = attribute.Value
			}
			stack = append(stack, element)
		case xml.EndElement:
			if len(stack) == 0 || stack[len(stack)-1].name != token.Name {
				return nil, nil
			}
			element := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				root = map[string]any{xmlQualifiedName(element.name): element.value()}
			} else {
				stack[len(stack)-1].add(xmlQualifiedName(element.name), element.value())
			}
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(token)) != "" {
					return nil, nil
				}
			} else if !strings.HasPrefix(text[before:], "<![CDATA[") {
				stack[len(stack)-1].text.Write(token)
			}
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxReplacementBytes {
		return nil, fmt.Errorf("parse_xml encoded object exceeds %d bytes", maxReplacementBytes)
	}
	return string(encoded), nil
}
