package jsondoc

// New creates a Store holding the given initial JSON document.
func New(initial []byte, opts Options) (*Store, error) {
	if opts.MaxNodes <= 0 {
		return nil, ErrInvalidOptions
	}
	doc, err := parseJSON(initial)
	if err != nil {
		return nil, err
	}
	if countNodes(doc) > opts.MaxNodes {
		return nil, ErrCapacity
	}
	return &Store{doc: doc, revision: 1, maxNodes: opts.MaxNodes}, nil
}

// Snapshot returns the current revision, node count and document.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Snapshot{
		Revision: s.revision,
		Nodes:    countNodes(s.doc),
		Document: marshalJSON(s.doc),
	}
}

// Apply validates the whole batch, checks the expected revision, then
// executes operations in order on an isolated deep copy. On any failure
// the store is left untouched and a zero Result is returned.
func (s *Store) Apply(expectedRevision uint64, ops []Operation) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	vops := make([]validatedOp, len(ops))
	for i, o := range ops {
		v, err := validateOp(o, i)
		if err != nil {
			return Result{}, err
		}
		vops[i] = v
	}
	if expectedRevision != s.revision {
		return Result{}, ErrRevisionConflict
	}

	doc := deepCopy(s.doc)
	mutating := false
	for i := range vops {
		if err := vops[i].exec(&doc); err != nil {
			return Result{}, &OpError{Index: i, Path: ops[i].Path, Err: err}
		}
		if vops[i].mutates() {
			mutating = true
		}
	}
	nodes := countNodes(doc)
	if nodes > s.maxNodes {
		return Result{}, ErrCapacity
	}
	s.doc = doc
	if mutating {
		s.revision++
	}
	return Result{Revision: s.revision, Nodes: nodes, Document: marshalJSON(doc)}, nil
}

// validatedOp is a structurally validated operation ready to execute.
type validatedOp struct {
	kind     string
	path     []string
	from     []string
	fromRaw  string
	pathRaw  string
	value    any
	hasValue bool
}

func (v *validatedOp) mutates() bool {
	return v.kind != "test"
}

func validateOp(o Operation, index int) (validatedOp, error) {
	fail := func(err error) (validatedOp, error) {
		return validatedOp{}, &OpError{Index: index, Path: o.Path, Err: err}
	}
	switch o.Op {
	case "add", "remove", "replace", "move", "copy", "test":
	default:
		return fail(ErrInvalidOperation)
	}
	v := validatedOp{kind: o.Op, pathRaw: o.Path, fromRaw: o.From}

	path, err := parsePointer(o.Path)
	if err != nil {
		return fail(err)
	}
	v.path = path

	switch o.Op {
	case "add", "replace", "test":
		if o.From != "" {
			return fail(ErrInvalidOperation)
		}
		val, err := parseJSON(o.Value)
		if err != nil {
			return fail(err)
		}
		v.value = val
		v.hasValue = true
	case "remove":
		if o.Value != nil || o.From != "" {
			return fail(ErrInvalidOperation)
		}
	case "move", "copy":
		if o.Value != nil {
			return fail(ErrInvalidOperation)
		}
		from, err := parsePointer(o.From)
		if err != nil {
			return fail(err)
		}
		v.from = from
	}
	return v, nil
}

func (v *validatedOp) exec(doc *any) error {
	switch v.kind {
	case "add":
		return addValue(doc, v.path, v.value)
	case "remove":
		return removeValue(doc, v.path)
	case "replace":
		return replaceValue(doc, v.path, v.value)
	case "test":
		target, err := getValue(*doc, v.path)
		if err != nil {
			return err
		}
		if !jsonEqual(target, v.value) {
			return ErrTestFailed
		}
		return nil
	case "copy":
		src, err := getValue(*doc, v.from)
		if err != nil {
			return err
		}
		return addValue(doc, v.path, deepCopy(src))
	case "move":
		if v.fromRaw == v.pathRaw {
			return nil
		}
		if isStrictDescendant(v.from, v.path) {
			return ErrMoveIntoChild
		}
		src, err := getValue(*doc, v.from)
		if err != nil {
			return err
		}
		if err := removeValue(doc, v.from); err != nil {
			return err
		}
		return addValue(doc, v.path, src)
	}
	return ErrInvalidOperation
}

// getValue resolves tokens against doc and returns the value found.
func getValue(doc any, tokens []string) (any, error) {
	cur := doc
	for _, tok := range tokens {
		switch c := cur.(type) {
		case map[string]any:
			val, ok := c[tok]
			if !ok {
				return nil, ErrNotFound
			}
			cur = val
		case []any:
			idx, err := parseIndex(tok)
			if err != nil {
				return nil, err
			}
			if idx >= len(c) {
				return nil, ErrInvalidIndex
			}
			cur = c[idx]
		default:
			return nil, ErrTypeMismatch
		}
	}
	return cur, nil
}

// parent resolves all but the last token and returns the container.
func parent(doc any, tokens []string) (any, error) {
	return getValue(doc, tokens[:len(tokens)-1])
}

func addValue(doc *any, tokens []string, val any) error {
	if len(tokens) == 0 {
		*doc = val
		return nil
	}
	p, err := parent(*doc, tokens)
	if err != nil {
		return err
	}
	last := tokens[len(tokens)-1]
	switch c := p.(type) {
	case map[string]any:
		c[last] = val
		return nil
	case []any:
		if last == "-" {
			*doc = setAt(doc, tokens[:len(tokens)-1], append(c, val))
			return nil
		}
		idx, err := parseIndex(last)
		if err != nil {
			return err
		}
		if idx > len(c) {
			return ErrInvalidIndex
		}
		grown := make([]any, 0, len(c)+1)
		grown = append(grown, c[:idx]...)
		grown = append(grown, val)
		grown = append(grown, c[idx:]...)
		*doc = setAt(doc, tokens[:len(tokens)-1], grown)
		return nil
	default:
		return ErrTypeMismatch
	}
}

func removeValue(doc *any, tokens []string) error {
	if len(tokens) == 0 {
		return ErrRootRemoval
	}
	p, err := parent(*doc, tokens)
	if err != nil {
		return err
	}
	last := tokens[len(tokens)-1]
	switch c := p.(type) {
	case map[string]any:
		if _, ok := c[last]; !ok {
			return ErrNotFound
		}
		delete(c, last)
		return nil
	case []any:
		idx, err := parseIndex(last)
		if err != nil {
			return err
		}
		if idx >= len(c) {
			return ErrInvalidIndex
		}
		shrunk := make([]any, 0, len(c)-1)
		shrunk = append(shrunk, c[:idx]...)
		shrunk = append(shrunk, c[idx+1:]...)
		*doc = setAt(doc, tokens[:len(tokens)-1], shrunk)
		return nil
	default:
		return ErrTypeMismatch
	}
}

func replaceValue(doc *any, tokens []string, val any) error {
	if len(tokens) == 0 {
		*doc = val
		return nil
	}
	p, err := parent(*doc, tokens)
	if err != nil {
		return err
	}
	last := tokens[len(tokens)-1]
	switch c := p.(type) {
	case map[string]any:
		if _, ok := c[last]; !ok {
			return ErrNotFound
		}
		c[last] = val
		return nil
	case []any:
		idx, err := parseIndex(last)
		if err != nil {
			return err
		}
		if idx >= len(c) {
			return ErrInvalidIndex
		}
		c[idx] = val
		return nil
	default:
		return ErrTypeMismatch
	}
}

// setAt replaces the value at tokens within *root, returning the new root.
func setAt(root *any, tokens []string, val any) any {
	if len(tokens) == 0 {
		return val
	}
	cur := *root
	for _, tok := range tokens[:len(tokens)-1] {
		switch c := cur.(type) {
		case map[string]any:
			cur = c[tok]
		case []any:
			idx, _ := parseIndex(tok)
			cur = c[idx]
		}
	}
	last := tokens[len(tokens)-1]
	switch c := cur.(type) {
	case map[string]any:
		c[last] = val
	case []any:
		idx, _ := parseIndex(last)
		c[idx] = val
	}
	return *root
}
