package reservation

import (
	"fmt"
	"sort"
	"sync"
)

const (
	maxNameBytes   = 64
	maxValueBytes  = 1 << 20
	maxScanLimit   = 1000
	maxResources   = 10000
	maxReservation = 1000000
	maxValueCap    = 64 << 20
)

// Ledger 是并发安全的内存区间预约账本。
//
// 内部按资源维护按 (Start,End,ID) 排序的预约切片（同一资源内区间互不
// 重叠，故按 Start 即有序），并用全局 ID 索引支持 O(log n) 定位。
type Ledger struct {
	mu         sync.RWMutex
	opts       Options
	generation uint64
	byID       map[string]*Reservation
	byResource map[string][]*Reservation
	valueBytes int
}

func New(o Options) (*Ledger, error) {
	if o.MaxResources < 1 || o.MaxResources > maxResources ||
		o.MaxReservations < 1 || o.MaxReservations > maxReservation ||
		o.MaxValueBytes < 1 || o.MaxValueBytes > maxValueCap {
		return nil, fmt.Errorf("%w: %+v", ErrInvalidOptions, o)
	}
	return &Ledger{
		opts:       o,
		byID:       make(map[string]*Reservation),
		byResource: make(map[string][]*Reservation),
	}, nil
}

func validName(s string) bool {
	if len(s) < 1 || len(s) > maxNameBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func validateReservation(r *Reservation) error {
	if !validName(r.ID) {
		return fmt.Errorf("%w: %q", ErrInvalidID, r.ID)
	}
	if !validName(r.Resource) {
		return fmt.Errorf("%w: %q", ErrInvalidResource, r.Resource)
	}
	if r.Start >= r.End {
		return fmt.Errorf("%w: [%d,%d)", ErrInvalidInterval, r.Start, r.End)
	}
	if len(r.Value) > maxValueBytes {
		return fmt.Errorf("%w: %d", ErrValueTooLarge, len(r.Value))
	}
	return nil
}

func validateChange(c *Change) error {
	switch c.Type {
	case ChangeAdd:
		if c.ID != "" {
			return fmt.Errorf("%w: add must not set ID", ErrInvalidChange)
		}
		return validateReservation(&c.Reservation)
	case ChangeDelete:
		if c.Reservation.ID != "" {
			return fmt.Errorf("%w: delete must not set Reservation.ID", ErrInvalidChange)
		}
		if !validName(c.ID) {
			return fmt.Errorf("%w: %q", ErrInvalidID, c.ID)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown type %d", ErrInvalidChange, c.Type)
	}
}

// less 按 (Start,End,ID) 排序。
func less(a, b *Reservation) bool {
	if a.Start != b.Start {
		return a.Start < b.Start
	}
	if a.End != b.End {
		return a.End < b.End
	}
	return a.ID < b.ID
}

func cloneReservation(r *Reservation) *Reservation {
	c := *r
	c.Value = append([]byte(nil), r.Value...)
	return &c
}

func (l *Ledger) cloneState() (map[string]*Reservation, map[string][]*Reservation) {
	byID := make(map[string]*Reservation, len(l.byID))
	byResource := make(map[string][]*Reservation, len(l.byResource))
	for id, r := range l.byID {
		byID[id] = r
	}
	for res, rs := range l.byResource {
		c := make([]*Reservation, len(rs))
		copy(c, rs)
		byResource[res] = c
	}
	return byID, byResource
}

func insertSorted(rs []*Reservation, r *Reservation) []*Reservation {
	i := sort.Search(len(rs), func(i int) bool { return less(r, rs[i]) })
	rs = append(rs, nil)
	copy(rs[i+1:], rs[i:])
	rs[i] = r
	return rs
}

func removeSorted(rs []*Reservation, r *Reservation) []*Reservation {
	i := sort.Search(len(rs), func(i int) bool { return !less(rs[i], r) })
	if i < len(rs) && rs[i].ID == r.ID {
		return append(rs[:i], rs[i+1:]...)
	}
	return rs
}

func (l *Ledger) Apply(c Change) (uint64, error) {
	return l.ApplyBatch([]Change{c})
}

func (l *Ledger) ApplyBatch(changes []Change) (uint64, error) {
	if len(changes) == 0 {
		l.mu.RLock()
		g := l.generation
		l.mu.RUnlock()
		return g, nil
	}
	// 结构校验：按输入顺序完成全部 change 的校验，先于任何语义处理。
	for i := range changes {
		if err := validateChange(&changes[i]); err != nil {
			return 0, err
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	byID, byResource := l.cloneState()
	valueBytes := l.valueBytes

	// 语义操作：在候选副本上按输入顺序执行。
	for i := range changes {
		c := &changes[i]
		switch c.Type {
		case ChangeAdd:
			if _, ok := byID[c.Reservation.ID]; ok {
				return 0, fmt.Errorf("%w: %q", ErrDuplicate, c.Reservation.ID)
			}
			r := cloneReservation(&c.Reservation)
			byID[r.ID] = r
			byResource[r.Resource] = insertSorted(byResource[r.Resource], r)
			valueBytes += len(r.Value)
		case ChangeDelete:
			r, ok := byID[c.ID]
			if !ok {
				return 0, fmt.Errorf("%w: %q", ErrNotFound, c.ID)
			}
			delete(byID, c.ID)
			byResource[r.Resource] = removeSorted(byResource[r.Resource], r)
			if len(byResource[r.Resource]) == 0 {
				delete(byResource, r.Resource)
			}
			valueBytes -= len(r.Value)
		}
	}

	// 最终状态检查：容量先于重叠。
	if len(byResource) > l.opts.MaxResources ||
		len(byID) > l.opts.MaxReservations ||
		valueBytes > l.opts.MaxValueBytes {
		return 0, fmt.Errorf("%w: resources=%d reservations=%d valueBytes=%d",
			ErrCapacity, len(byResource), len(byID), valueBytes)
	}
	for _, rs := range byResource {
		for i := 1; i < len(rs); i++ {
			if rs[i].Start < rs[i-1].End {
				return 0, fmt.Errorf("%w: %q overlaps %q",
					ErrConflict, rs[i-1].ID, rs[i].ID)
			}
		}
	}

	l.byID = byID
	l.byResource = byResource
	l.valueBytes = valueBytes
	l.generation++
	return l.generation, nil
}

func (l *Ledger) At(resource string, t int64) (AtResult, error) {
	if !validName(resource) {
		return AtResult{}, fmt.Errorf("%w: %q", ErrInvalidResource, resource)
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	res := AtResult{Generation: l.generation}
	rs := l.byResource[resource]
	// 最后一个 Start <= t 的预约（区间互不重叠，故唯一候选）。
	i := sort.Search(len(rs), func(i int) bool { return rs[i].Start > t }) - 1
	if i >= 0 && t < rs[i].End {
		res.Found = true
		res.Reservation = *cloneReservation(rs[i])
	}
	return res, nil
}

func (l *Ledger) Scan(resource string, from, to int64, limit int) (ScanResult, error) {
	if !validName(resource) {
		return ScanResult{}, fmt.Errorf("%w: %q", ErrInvalidResource, resource)
	}
	if from >= to || limit < 1 || limit > maxScanLimit {
		return ScanResult{}, fmt.Errorf("%w: [%d,%d) limit=%d", ErrInvalidScan, from, to, limit)
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	res := ScanResult{Generation: l.generation}
	rs := l.byResource[resource]
	// 与 [from,to) 重叠 ⇔ Start < to && End > from；从首个 End > from 起扫描。
	i := sort.Search(len(rs), func(i int) bool { return rs[i].End > from })
	for ; i < len(rs) && rs[i].Start < to && len(res.Reservations) < limit; i++ {
		res.Reservations = append(res.Reservations, *cloneReservation(rs[i]))
	}
	return res, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s := Snapshot{
		Generation:       l.generation,
		UsedReservations: len(l.byID),
		UsedValueBytes:   l.valueBytes,
	}
	names := make([]string, 0, len(l.byResource))
	for name := range l.byResource {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		rs := l.byResource[name]
		view := ResourceView{Resource: name, Reservations: make([]Reservation, len(rs))}
		for i, r := range rs {
			view.Reservations[i] = *cloneReservation(r)
		}
		s.Resources = append(s.Resources, view)
	}
	return s
}
