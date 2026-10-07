package crdt

import (
	"bytes"
	"encoding/json"
	"errors"
)

var (
	ErrWrongKind = errors.New("crdt: wrong container kind")
	ErrBadValue  = errors.New("crdt: bad JSON value")
	ErrBadKey    = errors.New("crdt: bad map key")
	ErrNoKey     = errors.New("crdt: no such key")
)

func (d *Doc) seqOf(p Parent, want Kind) (*seq, error) {
	c := d.container(p)
	if c == nil || c.kind != want {
		return nil, ErrWrongKind
	}
	return c.list, nil

}

func (d *Doc) insertAt(s *seq, pos int, tmpl InsertOp) (InsertOp, error) {
	if pos < 0 || pos > s.visibleCount() {
		return tmpl, ErrOutOfRange
	}

	left := s.start
	if pos > 0 {
		left = s.visibleAt(pos - 1)
	}

	right := left.right

	tmpl.ID = ID{Client: d.Client, Clock: d.clock}
	tmpl.Origin = left.ID
	tmpl.RightOrigin = right.ID

	d.clock++
	d.apply(tmpl)

	s.hintItem = d.items[tmpl.ID]
	s.hintIdx = pos

	return tmpl, nil
}

func (d *Doc) TextInsert(p Parent, pos int, s string) ([]Op, error) {
	if s == "" {
		return nil, nil
	}

	ops := make([]Op, 0)

	seq, err := d.seqOf(p, KindText)

	if err != nil {
		return nil, err
	}

	for idx, r := range s {
		op, err := d.insertAt(seq, pos+idx, InsertOp{Parent: p, Content: r})
		if err != nil {
			return ops, err
		}
		ops = append(ops, op)
	}

	return ops, nil

}

func (d *Doc) TextDelete(p Parent, pos, n int) ([]Op, error) {
	seq, err := d.seqOf(p, KindText)
	if err != nil {
		return nil, err
	}

	return d.deleteRange(seq, pos, n)
}

func (d *Doc) deleteRange(s *seq, pos, n int) ([]Op, error) {
	if pos < 0 || n < 0 || pos+n > s.visibleCount() {
		return nil, ErrOutOfRange
	}

	ops := make([]Op, 0, n)

	for i := 0; i < n; i++ {
		it := s.visibleAt(pos)

		op := DeleteOp{
			ID:     ID{Client: d.Client, Clock: d.clock},
			Target: it.ID,
		}
		d.clock++
		d.apply(op)

		s.hintItem = it
		s.hintIdx = pos

		ops = append(ops, op)
	}

	return ops, nil
}

func (d *Doc) ArrayInsertJSON(p Parent, pos int, value []byte) (InsertOp, error) {
	if !json.Valid(value) || len(value) > 64<<10 {
		return InsertOp{}, ErrBadValue
	}

	seq, err := d.seqOf(p, KindArray)
	if err != nil {
		return InsertOp{}, err
	}

	op, err := d.insertAt(seq, pos, InsertOp{Parent: p, CKind: ContentJSON, JSON: bytes.Clone(value)})

	if err != nil {
		return InsertOp{}, err
	}

	return op, nil

}

func (d *Doc) ArrayInsertType(p Parent, pos int, kind Kind) (InsertOp, error) {
	if kind != KindText && kind != KindArray && kind != KindMap {
		return InsertOp{}, ErrWrongKind
	}

	seq, err := d.seqOf(p, KindArray)
	if err != nil {
		return InsertOp{}, err
	}

	op, err := d.insertAt(seq, pos, InsertOp{Parent: p, CKind: ContentType, Type: kind})

	if err != nil {
		return InsertOp{}, err
	}

	return op, nil
}

func (d *Doc) ArrayDelete(p Parent, pos, n int) ([]Op, error) {
	seq, err := d.seqOf(p, KindArray)
	if err != nil {
		return nil, err
	}

	return d.deleteRange(seq, pos, n)

}

func (d *Doc) mapSeq(p Parent, key string) (*seq, error) {
	if key == "" || len(key) > 255 {
		return nil, ErrBadKey
	}

	c := d.container(p)
	if c == nil || c.kind != KindMap {
		return nil, ErrWrongKind
	}

	return d.seqFor(c, key), nil
}

func (d *Doc) mapSet(p Parent, key string, tmpl InsertOp) (InsertOp, error) {
	s, err := d.mapSeq(p, key)
	if err != nil {
		return InsertOp{}, err
	}

	tmpl.ID = ID{Client: d.Client, Clock: d.clock}
	tmpl.Origin = s.last().ID
	tmpl.RightOrigin = EndID
	tmpl.Parent = p
	tmpl.Key = key

	d.clock++
	d.apply(tmpl)

	return tmpl, nil
}

func (d *Doc) MapSetJSON(p Parent, key string, value []byte) (InsertOp, error) {
	if !json.Valid(value) || len(value) > 64<<10 {
		return InsertOp{}, ErrBadValue
	}

	return d.mapSet(p, key, InsertOp{CKind: ContentJSON, JSON: bytes.Clone(value)})
}

func (d *Doc) MapSetType(p Parent, key string, kind Kind) (InsertOp, error) {
	if kind != KindText && kind != KindArray && kind != KindMap {
		return InsertOp{}, ErrWrongKind
	}

	return d.mapSet(p, key, InsertOp{CKind: ContentType, Type: kind})
}

func (d *Doc) MapDelete(p Parent, key string) (DeleteOp, error) {
	if key == "" || len(key) > 255 {
		return DeleteOp{}, ErrBadKey
	}

	c := d.lookup(p)
	if c == nil {
		return DeleteOp{}, ErrNoKey
	}
	if c.kind != KindMap {
		return DeleteOp{}, ErrWrongKind
	}

	s := c.keys[key]
	if s == nil {
		return DeleteOp{}, ErrNoKey
	}

	last := s.last()
	if last == s.start || last.Deleted {
		return DeleteOp{}, ErrNoKey
	}

	op := DeleteOp{ID: ID{Client: d.Client, Clock: d.clock}, Target: last.ID}
	d.clock++
	d.apply(op)

	return op, nil
}

func (d *Doc) mapValue(c *container, key string) *Item {
	s := c.keys[key]
	if s == nil {
		return nil
	}

	last := s.last()
	if last == s.start || last.Deleted {
		return nil
	}

	return last
}

func (d *Doc) MapChild(p Parent, key string) (Parent, bool) {
	c := d.lookup(p)
	if c == nil || c.kind != KindMap {
		return Parent{}, false
	}

	v := d.mapValue(c, key)
	if v == nil || v.CKind != ContentType {
		return Parent{}, false
	}

	return ItemParent(v.ID), true
}

func (d *Doc) itemValue(it *Item) any {
	if it.CKind == ContentJSON {
		var v any
		_ = json.Unmarshal(it.JSON, &v)
		return v
	}

	return d.ToJSON(ItemParent(it.ID))
}

func (d *Doc) ToJSON(p Parent) any {
	c := d.lookup(p)
	if c == nil {
		return nil
	}

	switch c.kind {
	case KindText:
		var rs []rune
		for it := c.list.start.right; it != c.list.end; it = it.right {
			if !it.Deleted {
				rs = append(rs, it.Content)
			}
		}
		return string(rs)
	case KindArray:
		out := []any{}
		for it := c.list.start.right; it != c.list.end; it = it.right {
			if !it.Deleted {
				out = append(out, d.itemValue(it))
			}
		}
		return out
	case KindMap:
		m := map[string]any{}
		for key := range c.keys {
			if v := d.mapValue(c, key); v != nil {
				m[key] = d.itemValue(v)
			}
		}
		return m
	}

	return nil
}

func (d *Doc) JSON() ([]byte, error) {
	out := map[string]any{}

	for p, c := range d.containers {
		if !p.IsRoot() || c.isEmpty() {
			continue
		}
		out[p.Root+":"+p.RootKind.String()] = d.ToJSON(p)
	}

	return json.Marshal(out)
}
