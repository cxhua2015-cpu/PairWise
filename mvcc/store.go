package mvcc

import (
	"bytes"
	"sort"
	"strings"
)

// version is one historical version of a key. A nil value is a tombstone.
type version struct {
	value     []byte
	createRev uint64
	modRev    uint64
	version   uint64
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

func kvFrom(key string, v version) KV {
	return KV{
		Key:            key,
		Value:          v.value,
		CreateRevision: v.createRev,
		ModRevision:    v.modRev,
		Version:        v.version,
	}
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

// liveAt returns the version visible at rev, and whether the key was live.
func liveAt(versions []version, rev uint64) (version, bool) {
	i := sort.Search(len(versions), func(i int) bool { return versions[i].modRev > rev })
	if i == 0 {
		return version{}, false
	}
	v := versions[i-1]
	if v.value == nil {
		return version{}, false
	}
	return v, true
}

func (s *Store) checkRevision(rev uint64) (uint64, error) {
	if rev == 0 {
		return s.revision, nil
	}
	if rev > s.revision {
		return 0, ErrFutureRevision
	}
	if rev <= s.compactRev {
		return 0, ErrCompacted
	}
	return rev, nil
}

// commitWrite applies one effective write at rev to live state, history and events.
func (s *Store) commitWrite(key string, nv version, seq uint32) {
	old, ok := s.live[key]
	if nv.value == nil {
		if ok {
			s.liveBytes -= len(key) + len(old.value)
			delete(s.live, key)
		}
	} else {
		if ok {
			s.liveBytes -= len(old.value)
		} else {
			s.liveBytes += len(key)
		}
		s.liveBytes += len(nv.value)
		s.live[key] = nv
	}
	s.history[key] = append(s.history[key], nv)
	var ev Event
	if nv.value == nil {
		ev = Event{Type: EventDelete, Revision: nv.modRev, Sequence: seq}
		ev.KV = KV{Key: key, CreateRevision: nv.createRev, ModRevision: nv.modRev, Version: nv.version}
	} else {
		ev = Event{Type: EventPut, Revision: nv.modRev, Sequence: seq, KV: kvFrom(key, nv)}
	}
	if ok {
		prev := kvFrom(key, old)
		ev.Prev = &prev
	}
	s.events = append(s.events, ev)
}

func (s *Store) Put(key string, value []byte) (Event, error) {
	if key == "" {
		return Event{}, ErrInvalidKey
	}
	v := cloneBytes(value)
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.live[key]
	added := len(key) + len(v)
	if ok {
		added -= len(key) + len(old.value)
	}
	if s.liveBytes+added > s.maxLive {
		return Event{}, ErrCapacity
	}
	rev := s.revision + 1
	nv := version{value: v, modRev: rev, createRev: rev, version: 1}
	if ok {
		nv.createRev = old.createRev
		nv.version = old.version + 1
	}
	s.revision = rev
	s.commitWrite(key, nv, 0)
	return cloneEvent(s.events[len(s.events)-1]), nil
}

func (s *Store) Delete(key string) (Event, bool, error) {
	if key == "" {
		return Event{}, false, ErrInvalidKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.live[key]
	if !ok {
		return Event{}, false, nil
	}
	rev := s.revision + 1
	s.revision = rev
	s.commitWrite(key, version{createRev: old.createRev, modRev: rev, version: old.version}, 0)
	return cloneEvent(s.events[len(s.events)-1]), true, nil
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

func (s *Store) Range(key, end string, revision uint64) ([]KV, error) {
	if err := validateKeyRange(key, end); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rev, err := s.checkRevision(revision)
	if err != nil {
		return nil, err
	}
	var kvs []KV
	if end == "" {
		if v, ok := liveAt(s.history[key], rev); ok {
			kvs = append(kvs, cloneKV(kvFrom(key, v)))
		}
		return kvs, nil
	}
	keys := make([]string, 0, 8)
	for k := range s.history {
		if k >= key && k < end {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v, ok := liveAt(s.history[k], rev); ok {
			kvs = append(kvs, cloneKV(kvFrom(k, v)))
		}
	}
	return kvs, nil
}

func (s *Store) Watch(prefix string, afterRevision uint64, limit int) ([]Event, error) {
	if limit < 0 {
		return nil, ErrInvalidLimit
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if afterRevision > s.revision {
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
	if err := validateKeyRange(op.Key, op.End); err != nil {
		return err
	}
	if op.Revision != 0 {
		return ErrInvalidOp
	}
	if op.Type == OpPut && op.End != "" {
		return ErrInvalidOp
	}
	return nil
}

func resultOK(cmp int, r CompareResult) bool {
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

func evalCompare(v version, ok bool, c Compare) bool {
	switch c.Target {
	case CompareExists:
		exists := uint64(0)
		if ok {
			exists = 1
		}
		return resultOK(compareUint(exists, c.Revision), c.Result)
	case CompareValue:
		return resultOK(bytes.Compare(v.value, c.Value), c.Result)
	case CompareModRevision:
		return resultOK(compareUint(v.modRev, c.Revision), c.Result)
	case CompareVersion:
		return resultOK(compareUint(v.version, c.Revision), c.Result)
	}
	return false
}

func compareUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

type pendingWrite struct {
	key string
	nv  version
}

func (s *Store) Txn(compares []Compare, success, failure []Op) (TxnResponse, error) {
	for _, c := range compares {
		if err := validateCompare(c); err != nil {
			return TxnResponse{}, err
		}
	}
	for _, branch := range [2][]Op{success, failure} {
		for _, op := range branch {
			if err := validateOp(op); err != nil {
				return TxnResponse{}, err
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	succeeded := true
	for _, c := range compares {
		v, ok := s.live[c.Key]
		if !evalCompare(v, ok, c) {
			succeeded = false
			break
		}
	}
	branch := success
	if !succeeded {
		branch = failure
	}

	cand := make(map[string]version, len(s.live)+len(branch))
	for k, v := range s.live {
		cand[k] = v
	}
	liveBytes := s.liveBytes
	responses := make([]OpResponse, len(branch))
	var pending []pendingWrite

	put := func(key string, value []byte) {
		old, ok := cand[key]
		nv := version{value: cloneBytes(value), version: 1}
		if ok {
			nv.createRev = old.createRev
			nv.version = old.version + 1
			liveBytes -= len(old.value)
		} else {
			liveBytes += len(key)
		}
		liveBytes += len(nv.value)
		cand[key] = nv
		pending = append(pending, pendingWrite{key, nv})
	}
	del := func(key string) {
		old, ok := cand[key]
		if !ok {
			return
		}
		liveBytes -= len(key) + len(old.value)
		delete(cand, key)
		pending = append(pending, pendingWrite{key, version{createRev: old.createRev, version: old.version}})
	}

	for i, op := range branch {
		switch op.Type {
		case OpRange:
			keys := make([]string, 0, 4)
			for k := range cand {
				if op.End == "" {
					if k == op.Key {
						keys = append(keys, k)
					}
				} else if k >= op.Key && k < op.End {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			for _, k := range keys {
				responses[i].KVs = append(responses[i].KVs, cloneKV(kvFrom(k, cand[k])))
			}
		case OpPut:
			put(op.Key, op.Value)
			responses[i].KVs = []KV{cloneKV(kvFrom(op.Key, cand[op.Key]))}
		case OpDelete:
			if op.End == "" {
				if _, ok := cand[op.Key]; ok {
					del(op.Key)
					responses[i].Deleted = 1
				}
			} else {
				keys := make([]string, 0, 4)
				for k := range cand {
					if k >= op.Key && k < op.End {
						keys = append(keys, k)
					}
				}
				sort.Strings(keys)
				for _, k := range keys {
					del(k)
				}
				responses[i].Deleted = len(keys)
			}
		}
	}

	if len(pending) == 0 {
		return TxnResponse{Succeeded: succeeded, Revision: s.revision, Responses: responses}, nil
	}
	if liveBytes > s.maxLive {
		return TxnResponse{}, ErrCapacity
	}

	rev := s.revision + 1
	s.revision = rev
	events := make([]Event, 0, len(pending))
	for i, pw := range pending {
		pw.nv.modRev = rev
		if pw.nv.createRev == 0 {
			pw.nv.createRev = rev
		}
		s.commitWrite(pw.key, pw.nv, uint32(i))
		events = append(events, cloneEvent(s.events[len(s.events)-1]))
	}
	return TxnResponse{Succeeded: succeeded, Revision: rev, Responses: responses, Events: events}, nil
}

func (s *Store) Compact(revision uint64) error {
	if revision == 0 {
		return ErrInvalidRevision
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if revision > s.revision {
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
	for key, versions := range s.history {
		base := sort.Search(len(versions), func(i int) bool { return versions[i].modRev > revision })
		var out []version
		if base > 0 && versions[base-1].value != nil {
			out = append(out, versions[base-1])
		}
		out = append(out, versions[base:]...)
		if len(out) == 0 {
			delete(s.history, key)
		} else {
			s.history[key] = append([]version(nil), out...)
		}
	}
	return nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.live))
	for k := range s.live {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	snap := Snapshot{
		Revision:        s.revision,
		CompactRevision: s.compactRev,
		LiveBytes:       s.liveBytes,
	}
	for _, k := range keys {
		snap.KVs = append(snap.KVs, cloneKV(kvFrom(k, s.live[k])))
	}
	return snap
}
