package jsondoc

import (
	"bytes"
	"encoding/json"
	"io"
	"math/big"
	"sort"
	"strings"
)

// parseJSON decodes exactly one JSON value, rejecting trailing content.
func parseJSON(data []byte) (any, error) {
	if len(data) == 0 {
		return nil, ErrInvalidJSON
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, ErrInvalidJSON
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, ErrInvalidJSON
	}
	return v, nil
}

func deepCopy(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, val := range t {
			m[k] = deepCopy(val)
		}
		return m
	case []any:
		s := make([]any, len(t))
		for i, val := range t {
			s[i] = deepCopy(val)
		}
		return s
	default:
		return v
	}
}

func countNodes(v any) int {
	switch t := v.(type) {
	case map[string]any:
		n := 1
		for _, val := range t {
			n += countNodes(val)
		}
		return n
	case []any:
		n := 1
		for _, val := range t {
			n += countNodes(val)
		}
		return n
	default:
		return 1
	}
}

// marshalJSON writes compact JSON with sorted object keys; number
// lexemes are preserved as decoded.
func marshalJSON(v any) []byte {
	var sb strings.Builder
	writeValue(&sb, v)
	return []byte(sb.String())
}

func writeValue(sb *strings.Builder, v any) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sb.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			sb.Write(kb)
			sb.WriteByte(':')
			writeValue(sb, t[k])
		}
		sb.WriteByte('}')
	case []any:
		sb.WriteByte('[')
		for i, val := range t {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeValue(sb, val)
		}
		sb.WriteByte(']')
	case json.Number:
		sb.WriteString(t.String())
	case string:
		b, _ := json.Marshal(t)
		sb.Write(b)
	case bool:
		if t {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	default:
		sb.WriteString("null")
	}
}

// jsonEqual compares two JSON values semantically: object member order
// and number lexemes are insignificant; numbers compare by exact value.
func jsonEqual(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, val := range av {
			other, ok := bv[k]
			if !ok || !jsonEqual(val, other) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !jsonEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case json.Number:
		bv, ok := b.(json.Number)
		if !ok {
			return false
		}
		return numberEqual(av, bv)
	default:
		return a == b
	}
}

func numberEqual(a, b json.Number) bool {
	ra, okA := new(big.Rat).SetString(a.String())
	rb, okB := new(big.Rat).SetString(b.String())
	if okA && okB {
		return ra.Cmp(rb) == 0
	}
	return a.String() == b.String()
}
