package mvcc

import (
	"bytes"
	"sort"
	"strings"
	"sync"
)

// version is one historical record of a key. A nil value is a tombstone.
type version struct {
	value     []byte
	createRev uint64
	modRev    uint64
	version   uint64
}

// liveEntry is the current lifetime of a live key.
type liveEntry struct {
	value     []byte
	createRev uint64
	modRev    uint64
	version   uint64
}

type Store struct {
	mu         sync.Mutex
	maxLive    int
	liveBytes  int
	currentRev uint64
	compactRev uint64
	live       map[string]*liveEntry
	history    map[string][]version
	events     []Event
}

func New(opts Options) (*Store, error) {
	if opts.MaxLiveBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{
		maxLive: opts.MaxLiveBytes,
		live:    make(map[string]*liveEntry),
		history: make(map[string][]version),
	}, nil
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

func cloneKV(kv KV) KV {
	kv.Value = cloneBytes(kv.Value)
	return kv
}

func cloneEvent(e Event) Event {
	e.KV = cloneKV(e.KV)
	if e.Prev != nil {
		p := cloneKV(*e.Prev)
		e.Prev = &p
	}
	return e
}

func liveKV(key string, e *liveEntry) KV {
	return KV{
		Key:            key,
		Value:          cloneBytes(e.value),
		CreateRevision: e.createRev,
		ModRevision:    e.modRev,
		Version:        e.version,
	}
}

func validateExactKey(key string) error {
	if key == "" {
		return ErrInvalidKey
	}
	return nil
}

func validateKeyRange(key, end string) error {
	if key == "" {
		return ErrInvalidKey
	}
	if end != "" && end <= key {
		return ErrInvalidRange
	}
	return nil
}

// applyWrite records an effective write at rev into history and live state.
// The caller must hold the lock and pass already-owned value bytes.
func (s *Store) applyWrite(key string, value []byte, rev uint64) (kv KV, prev *KV) {
	old := s.live[key]
	if old != nil {
		p := liveKV(key, old)
		prev = &p
		s.liveBytes -= len(key) + len(old.value)
	}
	if value == nil {
		// delete: old must exist
		s.history[key] = append(s.history[key], version{
			value:     nil,
			createRev: old.createRev,
			modRev:    rev,
			version:   old.version,
		})
		delete(s.live, key)
		return KV{
			Key:            key,
			Value:          nil,
			CreateRevision: old.createRev,
			ModRevision:    rev,
			Version:        old.version,
		}, prev
	}
	entry := &liveEntry{value: value, modRev: rev}
	if old != nil {
		entry.createRev = old.createRev
		entry.version = old.version + 1
	} else {
		entry.createRev = rev
		entry.version = 1
	}
	s.live[key] = entry
	s.liveBytes += len(key) + len(value)
	s.history[key] = append(s.history[key], version{
		value:     value,
		createRev: entry.createRev,
		modRev:    rev,
		version:   entry.version,
	})
	return KV{
		Key:            key,
		Value:          cloneBytes(value),
		CreateRevision: entry.createRev,
		ModRevision:    rev,
		Version:        entry.version,
	}, prev
}

func (s *Store) appendEvent(typ EventType, kv KV, prev *KV, rev uint64, seq uint32) Event {
	e := Event{Type: typ, KV: kv, Prev: prev, Revision: rev, Sequence: seq}
	s.events = append(s.events, cloneEvent(e))
	return e
}

func (s *Store) Put(key string, value []byte) (Event, error) {
	if err := validateExactKey(key); err != nil {
		return Event{}, err
	}
	val := cloneBytes(value)
	s.mu.Lock()
	defer s.mu.Unlock()
	newLive := s.liveBytes + len(key) + len(val)
	if old := s.live[key]; old != nil {
		newLive -= len(key) + len(old.value)
	}
	if newLive > s.maxLive {
		return Event{}, ErrCapacity
	}
	rev := s.currentRev + 1
	kv, prev := s.applyWrite(key, val, rev)
	s.currentRev = rev
	return s.appendEvent(EventPut, kv, prev, rev, 0), nil
}

func (s *Store) Delete(key string) (Event, bool, error) {
	if err := validateExactKey(key); err != nil {
		return Event{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live[key] == nil {
		return Event{}, false, nil
	}
	rev := s.currentRev + 1
	kv, prev := s.applyWrite(key, nil, rev)
	s.currentRev = rev
	return s.appendEvent(EventDelete, kv, prev, rev, 0), true, nil
}

// kvAtRevision returns the visible KV for key at rev, and whether it is live.
func (s *Store) kvAtRevision(key string, rev uint64) (KV, bool) {
	vs := s.history[key]
	i := sort.Search(len(vs), func(i int) bool { return vs[i].modRev > rev }) - 1
	if i < 0 || vs[i].value == nil {
		return KV{}, false
	}
	return KV{
		Key:            key,
		Value:          cloneBytes(vs[i].value),
		CreateRevision: vs[i].createRev,
		ModRevision:    vs[i].modRev,
		Version:        vs[i].version,
	}, true
}

func (s *Store) Range(key, end string, revision uint64) ([]KV, error) {
	if err := validateKeyRange(key, end); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if revision > s.currentRev {
		return nil, ErrFutureRevision
	}
	if revision != 0 && revision <= s.compactRev {
		return nil, ErrCompacted
	}
	rev := revision
	if rev == 0 {
		rev = s.currentRev
	}
	var keys []string
	if end == "" {
		if _, ok := s.history[key]; ok {
			keys = []string{key}
		}
	} else {
		for k := range s.history {
			if k >= key && k < end {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
	}
	var out []KV
	for _, k := range keys {
		if kv, ok := s.kvAtRevision(k, rev); ok {
			out = append(out, kv)
		}
	}
	return out, nil
}

func (s *Store) Watch(prefix string, afterRevision uint64, limit int) ([]Event, error) {
	if limit < 0 {
		return nil, ErrInvalidLimit
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if afterRevision > s.currentRev {
		return nil, ErrFutureRevision
	}
	if afterRevision < s.compactRev {
		return nil, ErrCompacted
	}
	var out []Event
	for _, e := range s.events {
		if e.Revision <= afterRevision {
			continue
		}
		if !strings.HasPrefix(e.KV.Key, prefix) {
			continue
		}
		out = append(out, cloneEvent(e))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func validateCompare(c Compare) error {
	if c.Key == "" {
		return ErrInvalidKey
	}
	if c.Target < CompareExists || c.Target > CompareVersion {
		return ErrInvalidCompare
	}
	if c.Result < CompareEqual || c.Result > CompareGreater {
		return ErrInvalidCompare
	}
	if c.Target == CompareExists && c.Result != CompareEqual && c.Result != CompareNotEqual {
		return ErrInvalidCompare
	}
	return nil
}

func validateOp(op Op) error {
	if op.Type < OpRange || op.Type > OpDelete {
		return ErrInvalidOp
	}
	switch op.Type {
	case OpPut:
		if op.Key == "" {
			return ErrInvalidKey
		}
		if op.End != "" {
			return ErrInvalidRange
		}
	case OpRange, OpDelete:
		if err := validateKeyRange(op.Key, op.End); err != nil {
			return err
		}
	}
	if op.Revision != 0 {
		return ErrInvalidRevision
	}
	return nil
}

func compareInt(cmp int, r CompareResult) bool {
	switch r {
	case CompareEqual:
		return cmp == 0
	case CompareNotEqual:
		return cmp != 0
	case CompareLess:
		return cmp < 0
	case CompareGreater:
		return cmp > 0
	}
	return false
}

func evalCompare(c Compare, e *liveEntry) bool {
	switch c.Target {
	case CompareExists:
		exists := e != nil
		want := c.Revision != 0
		if c.Result == CompareEqual {
			return exists == want
		}
		return exists != want
	case CompareValue:
		var v []byte
		if e != nil {
			v = e.value
		}
		return compareInt(bytes.Compare(v, c.Value), c.Result)
	case CompareModRevision:
		var rev uint64
		if e != nil {
			rev = e.modRev
		}
		return compareUint(rev, c.Revision, c.Result)
	case CompareVersion:
		var ver uint64
		if e != nil {
			ver = e.version
		}
		return compareUint(ver, c.Revision, c.Result)
	}
	return false
}

func compareUint(a, b uint64, r CompareResult) bool {
	cmp := 0
	if a < b {
		cmp = -1
	} else if a > b {
		cmp = 1
	}
	return compareInt(cmp, r)
}

// candidate is the isolated transactional state for one branch.
type candidate struct {
	live      map[string]*liveEntry
	liveBytes int
}

func (s *Store) newCandidate() *candidate {
	c := &candidate{live: make(map[string]*liveEntry, len(s.live)), liveBytes: s.liveBytes}
	for k, e := range s.live {
		c.live[k] = e // entries are immutable during a txn; replaced not mutated
	}
	return c
}

func (c *candidate) put(key string, value []byte) (kv KV, prev *KV) {
	old := c.live[key]
	if old != nil {
		p := liveKV(key, old)
		prev = &p
		c.liveBytes -= len(key) + len(old.value)
	}
	entry := &liveEntry{value: value}
	if old != nil {
		entry.createRev = old.createRev
		entry.version = old.version + 1
	} else {
		entry.version = 1
	}
	c.live[key] = entry
	c.liveBytes += len(key) + len(value)
	return KV{Key: key, Value: cloneBytes(value), CreateRevision: entry.createRev, Version: entry.version}, prev
}

func (c *candidate) del(key string) (kv KV, prev *KV, ok bool) {
	old := c.live[key]
	if old == nil {
		return KV{}, nil, false
	}
	p := liveKV(key, old)
	c.liveBytes -= len(key) + len(old.value)
	delete(c.live, key)
	return KV{Key: key, CreateRevision: old.createRev, Version: old.version}, &p, true
}

func (c *candidate) rangeKeys(key, end string) []string {
	var keys []string
	if end == "" {
		if _, ok := c.live[key]; ok {
			keys = []string{key}
		}
		return keys
	}
	for k := range c.live {
		if k >= key && k < end {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// pendingWrite is an effective write staged by the selected branch.
type pendingWrite struct {
	key   string
	value []byte // nil means delete
	kv    KV     // event KV (tombstone for deletes)
	prev  *KV
}

func (s *Store) Txn(compares []Compare, success, failure []Op) (TxnResponse, error) {
	for _, c := range compares {
		if err := validateCompare(c); err != nil {
			return TxnResponse{}, err
		}
	}
	for _, op := range success {
		if err := validateOp(op); err != nil {
			return TxnResponse{}, err
		}
	}
	for _, op := range failure {
		if err := validateOp(op); err != nil {
			return TxnResponse{}, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	succeeded := true
	for _, c := range compares {
		if !evalCompare(c, s.live[c.Key]) {
			succeeded = false
			break
		}
	}
	ops := success
	if !succeeded {
		ops = failure
	}

	cand := s.newCandidate()
	responses := make([]OpResponse, 0, len(ops))
	var writes []pendingWrite

	for _, op := range ops {
		switch op.Type {
		case OpRange:
			var kvs []KV
			for _, k := range cand.rangeKeys(op.Key, op.End) {
				kvs = append(kvs, liveKV(k, cand.live[k]))
			}
			responses = append(responses, OpResponse{KVs: kvs})
		case OpPut:
			val := cloneBytes(op.Value)
			kv, prev := cand.put(op.Key, val)
			w := pendingWrite{key: op.Key, value: val, kv: kv, prev: prev}
			writes = append(writes, w)
			respKV := kv
			respKV.Value = cloneBytes(kv.Value)
			responses = append(responses, OpResponse{KVs: []KV{respKV}})
		case OpDelete:
			deleted := 0
			if op.End == "" {
				if kv, prev, ok := cand.del(op.Key); ok {
					deleted = 1
					writes = append(writes, pendingWrite{key: op.Key, value: nil, kv: kv, prev: prev})
				}
			} else {
				for _, k := range cand.rangeKeys(op.Key, op.End) {
					kv, prev, _ := cand.del(k)
					deleted++
					writes = append(writes, pendingWrite{key: k, value: nil, kv: kv, prev: prev})
				}
			}
			responses = append(responses, OpResponse{Deleted: deleted})
		}
	}

	if cand.liveBytes > s.maxLive {
		return TxnResponse{}, ErrCapacity
	}

	resp := TxnResponse{Succeeded: succeeded, Responses: responses}
	if len(writes) == 0 {
		resp.Revision = s.currentRev
		return resp, nil
	}

	rev := s.currentRev + 1
	for i, w := range writes {
		var typ EventType
		if w.value == nil {
			typ = EventDelete
		} else {
			typ = EventPut
		}
		// Commit into real state; use the store's own bookkeeping.
		kv, prev := s.applyWrite(w.key, w.value, rev)
		kv.Value = cloneBytes(kv.Value)
		e := s.appendEvent(typ, kv, prev, rev, uint32(i))
		resp.Events = append(resp.Events, e)
	}
	s.currentRev = rev
	resp.Revision = rev
	return resp, nil
}

func (s *Store) Compact(revision uint64) error {
	if revision == 0 {
		return ErrInvalidRevision
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if revision > s.currentRev {
		return ErrFutureRevision
	}
	if revision < s.compactRev {
		return ErrInvalidRevision
	}
	if revision == s.compactRev {
		return nil
	}
	s.compactRev = revision

	kept := s.events[:0]
	for _, e := range s.events {
		if e.Revision > revision {
			kept = append(kept, e)
		}
	}
	for i := len(kept); i < len(s.events); i++ {
		s.events[i] = Event{}
	}
	s.events = kept

	for key, vs := range s.history {
		base := sort.Search(len(vs), func(i int) bool { return vs[i].modRev > revision }) - 1
		if base < 0 {
			continue
		}
		rest := make([]version, len(vs)-base)
		copy(rest, vs[base:])
		s.history[key] = rest
	}
	return nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := Snapshot{
		Revision:        s.currentRev,
		CompactRevision: s.compactRev,
		LiveBytes:       s.liveBytes,
	}
	keys := make([]string, 0, len(s.live))
	for k := range s.live {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		snap.KVs = append(snap.KVs, liveKV(k, s.live[k]))
	}
	return snap
}
