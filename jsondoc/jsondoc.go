package jsondoc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"sort"
	"strconv"
	"sync"
)

var (
	ErrNotImplemented   = errors.New("jsondoc: not implemented")
	ErrInvalidOptions   = errors.New("jsondoc: invalid options")
	ErrInvalidJSON      = errors.New("jsondoc: invalid json")
	ErrInvalidOperation = errors.New("jsondoc: invalid operation")
	ErrInvalidPointer   = errors.New("jsondoc: invalid pointer")
	ErrInvalidIndex     = errors.New("jsondoc: invalid array index")
	ErrNotFound         = errors.New("jsondoc: not found")
	ErrTypeMismatch     = errors.New("jsondoc: type mismatch")
	ErrRootRemoval      = errors.New("jsondoc: cannot remove root")
	ErrMoveIntoChild    = errors.New("jsondoc: move into child")
	ErrTestFailed       = errors.New("jsondoc: test failed")
	ErrRevisionConflict = errors.New("jsondoc: revision conflict")
	ErrCapacity         = errors.New("jsondoc: capacity exceeded")
)

type Options struct{ MaxNodes int }
type Operation struct {
	Op, Path, From string
	Value          json.RawMessage
}
type Result struct {
	Revision uint64
	Nodes    int
	Document []byte
}
type Snapshot struct {
	Revision uint64
	Nodes    int
	Document []byte
}
type OpError struct {
	Index int
	Path  string
	Err   error
}

func (e *OpError) Error() string {
	return fmt.Sprintf("jsondoc: op %d path %q: %v", e.Index, e.Path, e.Err)
}
func (e *OpError) Unwrap() error { return e.Err }

// Store is a concurrency-safe in-memory JSON document with JSON Patch support.
type Store struct {
	mu       sync.Mutex
	doc      any
	revision uint64
	maxNodes int
}

func New(initial []byte, opts Options) (*Store, error) {
	if opts.MaxNodes <= 0 {
		return nil, ErrInvalidOptions
	}
	doc, err := decodeValue(initial)
	if err != nil {
		return nil, ErrInvalidJSON
	}
	if countNodes(doc) > opts.MaxNodes {
		return nil, ErrCapacity
	}
	return &Store{doc: doc, revision: 1, maxNodes: opts.MaxNodes}, nil
}

func (s *Store) Apply(expectedRevision uint64, ops []Operation) (Result, error) {
	parsed := make([]parsedOp, len(ops))
	for i, op := range ops {
		p, err := validateOp(op)
		if err != nil {
			return Result{}, &OpError{Index: i, Path: op.Path, Err: err}
		}
		parsed[i] = p
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if expectedRevision != s.revision {
		return Result{}, ErrRevisionConflict
	}
	work := deepCopy(s.doc)
	mutating := false
	for i := range parsed {
		if err := execute(&work, parsed[i]); err != nil {
			return Result{}, &OpError{Index: i, Path: parsed[i].op.Path, Err: err}
		}
		if parsed[i].op.Op != "test" {
			mutating = true
		}
	}
	nodes := countNodes(work)
	if nodes > s.maxNodes {
		return Result{}, ErrCapacity
	}
	s.doc = work
	if mutating {
		s.revision++
	}
	return Result{Revision: s.revision, Nodes: nodes, Document: marshalJSON(work)}, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Snapshot{Revision: s.revision, Nodes: countNodes(s.doc), Document: marshalJSON(s.doc)}
}

// ---- decoding / encoding ----

func decodeValue(data []byte) (any, error) {
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

func marshalJSON(v any) []byte {
	return appendJSON(nil, v)
}

func appendJSON(dst []byte, v any) []byte {
	switch t := v.(type) {
	case nil:
		return append(dst, "null"...)
	case bool:
		return strconv.AppendBool(dst, t)
	case string:
		b, _ := json.Marshal(t)
		return append(dst, b...)
	case json.Number:
		return append(dst, t.String()...)
	case map[string]any:
		dst = append(dst, '{')
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			kb, _ := json.Marshal(k)
			dst = append(dst, kb...)
			dst = append(dst, ':')
			dst = appendJSON(dst, t[k])
		}
		return append(dst, '}')
	case []any:
		dst = append(dst, '[')
		for i, e := range t {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendJSON(dst, e)
		}
		return append(dst, ']')
	default:
		return append(dst, "null"...)
	}
}

func countNodes(v any) int {
	n := 1
	switch t := v.(type) {
	case map[string]any:
		for _, e := range t {
			n += countNodes(e)
		}
	case []any:
		for _, e := range t {
			n += countNodes(e)
		}
	}
	return n
}

func deepCopy(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, e := range t {
			m[k] = deepCopy(e)
		}
		return m
	case []any:
		s := make([]any, len(t))
		for i, e := range t {
			s[i] = deepCopy(e)
		}
		return s
	default:
		return v
	}
}

// ---- JSON Pointer ----

func parsePointer(p string) ([]string, error) {
	if p == "" {
		return nil, nil
	}
	if p[0] != '/' {
		return nil, ErrInvalidPointer
	}
	raw := splitTokens(p[1:])
	tokens := make([]string, len(raw))
	for i, tok := range raw {
		dec, err := unescapeToken(tok)
		if err != nil {
			return nil, err
		}
		tokens[i] = dec
	}
	return tokens, nil
}

func splitTokens(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func unescapeToken(tok string) (string, error) {
	has := false
	for i := 0; i < len(tok); i++ {
		if tok[i] == '~' {
			has = true
			break
		}
	}
	if !has {
		return tok, nil
	}
	buf := make([]byte, 0, len(tok))
	for i := 0; i < len(tok); i++ {
		if tok[i] != '~' {
			buf = append(buf, tok[i])
			continue
		}
		if i+1 >= len(tok) {
			return "", ErrInvalidPointer
		}
		switch tok[i+1] {
		case '0':
			buf = append(buf, '~')
		case '1':
			buf = append(buf, '/')
		default:
			return "", ErrInvalidPointer
		}
		i++
	}
	return string(buf), nil
}

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
		n = n*10 + int(c-'0')
		if n > int(^uint(0)>>1)/2 {
			return 0, ErrInvalidIndex
		}
	}
	return n, nil
}

// ---- validation ----

type parsedOp struct {
	op      Operation
	path    []string
	from    []string
	hasFrom bool
	value   any
}

func validateOp(op Operation) (parsedOp, error) {
	var p parsedOp
	p.op = op
	switch op.Op {
	case "add", "remove", "replace", "move", "copy", "test":
	default:
		return p, ErrInvalidOperation
	}
	path, err := parsePointer(op.Path)
	if err != nil {
		return p, err
	}
	p.path = path
	switch op.Op {
	case "add", "replace", "test":
		if op.From != "" {
			return p, ErrInvalidOperation
		}
		v, err := decodeValue(op.Value)
		if err != nil {
			return p, ErrInvalidJSON
		}
		p.value = v
	case "remove":
		if op.From != "" || op.Value != nil {
			return p, ErrInvalidOperation
		}
	case "move", "copy":
		if op.Value != nil {
			return p, ErrInvalidOperation
		}
		from, err := parsePointer(op.From)
		if err != nil {
			return p, err
		}
		p.from = from
		p.hasFrom = true
	}
	return p, nil
}

// ---- execution ----

// lookup walks all tokens and returns the value at the location.
func lookup(doc any, tokens []string) (any, error) {
	cur := doc
	for _, tok := range tokens {
		switch c := cur.(type) {
		case map[string]any:
			v, ok := c[tok]
			if !ok {
				return nil, ErrNotFound
			}
			cur = v
		case []any:
			idx, err := parseIndex(tok)
			if err != nil {
				return nil, err
			}
			if idx >= len(c) {
				return nil, ErrNotFound
			}
			cur = c[idx]
		default:
			return nil, ErrTypeMismatch
		}
	}
	return cur, nil
}

// parentOf walks all but the last token and returns the container and last token.
func parentOf(doc any, tokens []string) (any, string, error) {
	parent, err := lookup(doc, tokens[:len(tokens)-1])
	if err != nil {
		return nil, "", err
	}
	switch parent.(type) {
	case map[string]any, []any:
		return parent, tokens[len(tokens)-1], nil
	default:
		return nil, "", ErrTypeMismatch
	}
}

func execAdd(doc *any, tokens []string, val any) error {
	if len(tokens) == 0 {
		*doc = val
		return nil
	}
	parent, last, err := parentOf(*doc, tokens)
	if err != nil {
		return err
	}
	switch c := parent.(type) {
	case map[string]any:
		c[last] = val
	case []any:
		if last == "-" {
			return setSlice(doc, tokens[:len(tokens)-1], append(c, val))
		}
		idx, err := parseIndex(last)
		if err != nil {
			return err
		}
		if idx > len(c) {
			return ErrNotFound
		}
		nc := make([]any, 0, len(c)+1)
		nc = append(nc, c[:idx]...)
		nc = append(nc, val)
		nc = append(nc, c[idx:]...)
		return setSlice(doc, tokens[:len(tokens)-1], nc)
	}
	return nil
}

// setSlice replaces the (possibly grown/shrunk) slice located at tokens within doc.
func setSlice(doc *any, tokens []string, ns []any) error {
	if len(tokens) == 0 {
		*doc = ns
		return nil
	}
	parent, last, err := parentOf(*doc, tokens)
	if err != nil {
		return err
	}
	switch c := parent.(type) {
	case map[string]any:
		c[last] = ns
	case []any:
		idx, err := parseIndex(last)
		if err != nil {
			return err
		}
		if idx >= len(c) {
			return ErrNotFound
		}
		c[idx] = ns
	}
	return nil
}

func execRemove(doc *any, tokens []string) (any, error) {
	if len(tokens) == 0 {
		return nil, ErrRootRemoval
	}
	parent, last, err := parentOf(*doc, tokens)
	if err != nil {
		return nil, err
	}
	switch c := parent.(type) {
	case map[string]any:
		v, ok := c[last]
		if !ok {
			return nil, ErrNotFound
		}
		delete(c, last)
		return v, nil
	case []any:
		idx, err := parseIndex(last)
		if err != nil {
			return nil, err
		}
		if idx >= len(c) {
			return nil, ErrNotFound
		}
		v := c[idx]
		ns := make([]any, 0, len(c)-1)
		ns = append(ns, c[:idx]...)
		ns = append(ns, c[idx+1:]...)
		if err := setSlice(doc, tokens[:len(tokens)-1], ns); err != nil {
			return nil, err
		}
		return v, nil
	}
	return nil, ErrTypeMismatch
}

func execReplace(doc *any, tokens []string, val any) error {
	if len(tokens) == 0 {
		*doc = val
		return nil
	}
	parent, last, err := parentOf(*doc, tokens)
	if err != nil {
		return err
	}
	switch c := parent.(type) {
	case map[string]any:
		if _, ok := c[last]; !ok {
			return ErrNotFound
		}
		c[last] = val
	case []any:
		idx, err := parseIndex(last)
		if err != nil {
			return err
		}
		if idx >= len(c) {
			return ErrNotFound
		}
		c[idx] = val
	}
	return nil
}

func isStrictPrefix(prefix, full []string) bool {
	if len(prefix) >= len(full) {
		return false
	}
	for i := range prefix {
		if prefix[i] != full[i] {
			return false
		}
	}
	return true
}

func execute(doc *any, p parsedOp) error {
	switch p.op.Op {
	case "add":
		return execAdd(doc, p.path, deepCopy(p.value))
	case "replace":
		return execReplace(doc, p.path, deepCopy(p.value))
	case "remove":
		_, err := execRemove(doc, p.path)
		return err
	case "copy":
		v, err := lookup(*doc, p.from)
		if err != nil {
			return err
		}
		return execAdd(doc, p.path, deepCopy(v))
	case "move":
		if p.op.From == p.op.Path {
			return nil
		}
		if isStrictPrefix(p.from, p.path) {
			return ErrMoveIntoChild
		}
		v, err := execRemove(doc, p.from)
		if err != nil {
			return err
		}
		return execAdd(doc, p.path, v)
	case "test":
		v, err := lookup(*doc, p.path)
		if err != nil {
			return err
		}
		if !jsonEqual(v, p.value) {
			return ErrTestFailed
		}
		return nil
	}
	return ErrInvalidOperation
}

// ---- semantic equality ----

func jsonEqual(a, b any) bool {
	switch av := a.(type) {
	case nil:
		return b == nil
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case json.Number:
		bv, ok := b.(json.Number)
		return ok && numberEqual(av, bv)
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, e := range av {
			be, ok := bv[k]
			if !ok || !jsonEqual(e, be) {
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
	}
	return false
}

func numberEqual(a, b json.Number) bool {
	ra, okA := new(big.Rat).SetString(a.String())
	rb, okB := new(big.Rat).SetString(b.String())
	if okA && okB {
		return ra.Cmp(rb) == 0
	}
	fa, errA := a.Float64()
	fb, errB := b.Float64()
	if errA != nil || errB != nil {
		return a.String() == b.String()
	}
	return fa == fb
}
