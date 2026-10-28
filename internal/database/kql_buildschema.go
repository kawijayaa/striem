package database

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// schemaNode is the merged schema of the values seen at one position:
// scalar type names, an object schema and an array element schema, in the
// order first seen.
type schemaNode struct {
	order   []string // "t:<type>", "object" or "array"
	types   map[string]bool
	keys    []string
	fields  map[string]*schemaNode
	indexer *schemaNode
}

const maxSchemaNodes = 1 << 16

func newSchemaNode() *schemaNode { return &schemaNode{types: map[string]bool{}, fields: map[string]*schemaNode{}} }

func (n *schemaNode) note(kind string) {
	if !n.types[kind] {
		n.types[kind] = true
		n.order = append(n.order, kind)
	}
}

// add merges a decoded JSON value, counting nodes against the budget.
func (n *schemaNode) add(value any, budget *int) error {
	if *budget--; *budget < 0 {
		return fmt.Errorf("buildschema exceeds %d schema nodes", maxSchemaNodes)
	}
	switch v := value.(type) {
	case nil:
		return nil
	case bool:
		n.note("t:bool")
	case json.Number:
		if strings.ContainsAny(v.String(), ".eE") {
			n.note("t:double")
		} else {
			n.note("t:long")
		}
	case string:
		n.note("t:string")
	case orderedObject:
		n.note("object")
		for _, entry := range v {
			if err := n.field(entry.key).add(entry.value, budget); err != nil {
				return err
			}
		}
	case []any:
		n.note("array")
		if n.indexer == nil {
			n.indexer = newSchemaNode()
		}
		for _, item := range v {
			if err := n.indexer.add(item, budget); err != nil {
				return err
			}
		}
	}
	return nil
}

func (n *schemaNode) field(key string) *schemaNode {
	if _, ok := n.fields[key]; !ok {
		n.fields[key] = newSchemaNode()
		n.keys = append(n.keys, key)
	}
	return n.fields[key]
}

func (n *schemaNode) write(buffer *bytes.Buffer) {
	parts := make([]func(), 0, len(n.order))
	for _, kind := range n.order {
		switch kind {
		case "object":
			parts = append(parts, func() {
				buffer.WriteByte('{')
				written := 0
				for _, key := range n.keys {
					child := n.fields[key]
					if len(child.order) == 0 {
						continue
					}
					if written > 0 {
						buffer.WriteByte(',')
					}
					encoded, _ := json.Marshal(key)
					buffer.Write(encoded)
					buffer.WriteByte(':')
					child.write(buffer)
					written++
				}
				buffer.WriteByte('}')
			})
		case "array":
			parts = append(parts, func() {
				buffer.WriteString("{\"`indexer`\":")
				if n.indexer == nil || len(n.indexer.order) == 0 {
					buffer.WriteString("null")
				} else {
					n.indexer.write(buffer)
				}
				buffer.WriteByte('}')
			})
		default:
			name := strings.TrimPrefix(kind, "t:")
			parts = append(parts, func() {
				encoded, _ := json.Marshal(name)
				buffer.Write(encoded)
			})
		}
	}
	if len(parts) == 1 {
		parts[0]()
		return
	}
	buffer.WriteByte('[')
	for i, part := range parts {
		if i > 0 {
			buffer.WriteByte(',')
		}
		part()
	}
	buffer.WriteByte(']')
}

type orderedEntry struct {
	key   string
	value any
}

type orderedObject []orderedEntry

// decodeOrdered decodes JSON keeping object key order and number text.
func decodeOrdered(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch delimiter := token.(type) {
	case json.Delim:
		switch delimiter {
		case '{':
			var object orderedObject
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				value, err := decodeOrdered(decoder)
				if err != nil {
					return nil, err
				}
				object = append(object, orderedEntry{keyToken.(string), value})
			}
			_, err := decoder.Token()
			return object, err
		case '[':
			var array []any
			for decoder.More() {
				value, err := decodeOrdered(decoder)
				if err != nil {
					return nil, err
				}
				array = append(array, value)
			}
			if array == nil {
				array = []any{}
			}
			_, err := decoder.Token()
			return array, err
		}
	}
	return token, nil
}

// buildSchemaAggregate implements buildschema over dynamic values.
type buildSchemaAggregate struct {
	root   *schemaNode
	budget int
}

func (a *buildSchemaAggregate) Step(value any) error {
	if a.root == nil {
		a.root, a.budget = newSchemaNode(), maxSchemaNodes
	}
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) {
		if number, isNumber := value.(int64); isNumber {
			return a.root.add(json.Number(fmt.Sprint(number)), &a.budget)
		}
		if number, isNumber := value.(float64); isNumber {
			return a.root.add(json.Number(fmt.Sprintf("%g.0", number)), &a.budget)
		}
		return nil
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	decoded, err := decodeOrdered(decoder)
	if err != nil {
		return a.root.add(text, &a.budget)
	}
	return a.root.add(decoded, &a.budget)
}

func (a *buildSchemaAggregate) Done() (any, error) {
	if a.root == nil || len(a.root.order) == 0 {
		return nil, nil
	}
	var buffer bytes.Buffer
	a.root.write(&buffer)
	return buffer.String(), nil
}
