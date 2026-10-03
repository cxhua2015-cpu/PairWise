package jsondoc

import (
	"math"
	"strings"
)

// parsePointer decodes an RFC 6901 JSON Pointer into tokens.
// The empty string denotes the root and yields nil tokens.
func parsePointer(p string) ([]string, error) {
	if p == "" {
		return nil, nil
	}
	if p[0] != '/' {
		return nil, ErrInvalidPointer
	}
	raw := strings.Split(p[1:], "/")
	tokens := make([]string, len(raw))
	for i, tok := range raw {
		if !strings.ContainsRune(tok, '~') {
			tokens[i] = tok
			continue
		}
		var b strings.Builder
		b.Grow(len(tok))
		for j := 0; j < len(tok); j++ {
			if tok[j] != '~' {
				b.WriteByte(tok[j])
				continue
			}
			if j+1 >= len(tok) {
				return nil, ErrInvalidPointer
			}
			switch tok[j+1] {
			case '0':
				b.WriteByte('~')
			case '1':
				b.WriteByte('/')
			default:
				return nil, ErrInvalidPointer
			}
			j++
		}
		tokens[i] = b.String()
	}
	return tokens, nil
}

// parseIndex validates a decimal array index: "0" or a non-zero digit
// followed by digits. Signs, leading zeros and overflow are rejected.
func parseIndex(tok string) (int, error) {
	if tok == "" {
		return 0, ErrInvalidIndex
	}
	if tok == "0" {
		return 0, nil
	}
	if tok[0] < '1' || tok[0] > '9' {
		return 0, ErrInvalidIndex
	}
	n := 0
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		if c < '0' || c > '9' {
			return 0, ErrInvalidIndex
		}
		d := int(c - '0')
		if n > (math.MaxInt-d)/10 {
			return 0, ErrInvalidIndex
		}
		n = n*10 + d
	}
	return n, nil
}

// isStrictDescendant reports whether path is strictly below from.
func isStrictDescendant(from, path []string) bool {
	if len(path) <= len(from) {
		return false
	}
	for i := range from {
		if from[i] != path[i] {
			return false
		}
	}
	return true
}
