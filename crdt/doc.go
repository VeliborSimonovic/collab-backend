package crdt

import (
	"encoding/json"
	"maps"
	"strings"
)

type Doc struct {
	Client     ClientID
	clock      uint64
	items      map[ID]*Item
	sv         map[ClientID]uint64
	log        map[ClientID][]Op
	pending    []Op
	itemCount  int
	opCount    int
	containers map[Parent]*container
}

type status int

const (
	duplicate status = iota
	notReady
	ready
)

func NewDoc(client ClientID) *Doc {
	if client == 0 {
		panic("Client is 0")
	}

	d := &Doc{
		Client: client,
		items:  make(map[ID]*Item),
		sv:     make(map[ClientID]uint64),
		log:    make(map[ClientID][]Op),
	}

	d.containers = make(map[Parent]*container)
	d.containers[DefaultText()] = &container{kind: KindText, list: newSeq()}

	return d
}

func (d *Doc) text() *seq {
	return d.containers[DefaultText()].list
}

func (d *Doc) LocalInsert(pos int, r rune) (InsertOp, error) {
	ops, err := d.TextInsert(DefaultText(), pos, string(r))
	if err != nil {
		return InsertOp{}, err
	}
	return ops[0].(InsertOp), nil
}

func (d *Doc) LocalDelete(pos int) (DeleteOp, error) {
	ops, err := d.TextDelete(DefaultText(), pos, 1)
	if err != nil {
		return DeleteOp{}, err
	}
	return ops[0].(DeleteOp), nil
}

func (d *Doc) String() string {
	var b strings.Builder
	s := d.text()

	for it := s.start.right; it != s.end; it = it.right {
		if !it.Deleted {
			b.WriteRune(it.Content)
		}

	}

	return b.String()

}

func (d *Doc) Order() []ID {
	s := d.text()

	arr := make([]ID, 0, len(d.items))

	for it := s.start.right; it != s.end; it = it.right {
		arr = append(arr, it.ID)
	}

	return arr

}

func (d *Doc) resolve(id ID, s *seq) *Item {
	switch id {
	case StartID:
		return s.start
	case EndID:
		return s.end
	default:
		return d.items[id]
	}
}

func (d *Doc) integrate(item *Item, s *seq) {
	left := d.resolve(item.Origin, s)
	right := d.resolve(item.RightOrigin, s)

	if left.right == right {
		d.items[item.ID] = item
		left.right = item
		right.left = item
		item.left = left
		item.right = right
		return
	}

	var o *Item

	scanned := make(map[ID]bool)
	conflicting := make(map[ID]bool)

	for o = left.right; o != right && o != s.end; o = o.right {
		scanned[o.ID] = true
		conflicting[o.ID] = true

		if o.Origin == item.Origin {
			if o.ID.Client < item.ID.Client {
				left = o
				clear(conflicting)
			} else if o.RightOrigin == item.RightOrigin {
				break
			}

		} else if scanned[o.Origin] {
			if !conflicting[o.Origin] {
				left = o
				clear(conflicting)
			}
		} else {
			break
		}
	}

	d.items[item.ID] = item
	right = left.right
	item.left, item.right = left, right
	left.right = item
	right.left = item

}

func (d *Doc) Receive(ops ...Op) (applied []Op) {
	d.pending = append(d.pending, ops...)

	for {
		progress := false
		keep := d.pending[:0]
		for _, op := range d.pending {
			switch d.readiness(op) {
			case duplicate:

			case ready:
				d.apply(op)
				applied = append(applied, op)
				progress = true
			case notReady:
				keep = append(keep, op)
			}
		}

		d.pending = keep
		if !progress {
			break
		}
	}
	return applied

}

func (d *Doc) checkInsert(op InsertOp) status {
	if !op.Parent.IsRoot() && d.items[op.Parent.Item] == nil {
		return notReady
	}

	k, ok := d.kindOf(op.Parent)
	if !ok {
		return duplicate
	}

	switch k {
	case KindText:
		if op.CKind != ContentRune || op.Key != "" {
			return duplicate
		}
	case KindArray:
		if (op.CKind != ContentJSON && op.CKind != ContentType) || op.Key != "" {
			return duplicate
		}
	case KindMap:
		if (op.CKind != ContentJSON && op.CKind != ContentType) || op.Key == "" {
			return duplicate
		}
	default:
		return duplicate
	}

	if op.CKind == ContentJSON {
		if !json.Valid(op.JSON) || len(op.JSON) > 64<<10 {
			return duplicate
		}
	}
	if op.CKind == ContentType {
		if op.Type != KindText && op.Type != KindArray && op.Type != KindMap {
			return duplicate
		}
	}

	for _, id := range [2]ID{op.Origin, op.RightOrigin} {
		if id == StartID || id == EndID {
			continue
		}
		it := d.items[id]
		if it == nil {
			return notReady
		}
		if it.Parent != op.Parent || it.Key != op.Key {
			return duplicate
		}
	}

	return ready
}

func (d *Doc) readiness(op Op) status {
	id := op.OpID()

	if id.Client == 0 {
		return duplicate
	}

	next := d.sv[id.Client]
	if id.Clock < next {
		return duplicate
	}
	if id.Clock > next {
		return notReady
	}

	switch o := op.(type) {
	case InsertOp:
		if o.Origin == EndID || o.RightOrigin == StartID {
			return duplicate
		}
		return d.checkInsert(o)
	case DeleteOp:
		if o.Target == StartID || o.Target == EndID {
			return duplicate
		}
		if d.items[o.Target] == nil {
			return notReady
		}
	default:
		return duplicate
	}

	return ready
}

func (d *Doc) apply(op Op) {

	switch v := op.(type) {
	case InsertOp:
		item := &Item{
			ID:          v.ID,
			Origin:      v.Origin,
			RightOrigin: v.RightOrigin,
			Content:     v.Content,
			Parent:      v.Parent,
			Key:         v.Key,
			CKind:       v.CKind,
			JSON:        v.JSON,
			Type:        v.Type,
		}

		c := d.container(v.Parent)
		s := d.seqFor(c, v.Key)

		d.integrate(item, s)
		d.items[v.ID] = item
		item.seq = s

		s.n++
		s.hintItem = s.start
		s.hintIdx = 0

		if v.CKind == ContentType {
			d.containers[ItemParent(v.ID)] = &container{kind: v.Type, list: newSeq()}
		}

		d.itemCount++
	case DeleteOp:
		if it := d.items[v.Target]; it != nil {
			if !it.Deleted {
				it.Deleted = true
				it.seq.n--
				it.seq.hintItem = it.seq.start
				it.seq.hintIdx = 0
			}
			it.Deleted = true
		}

	default:
		return

	}

	id := op.OpID()
	d.sv[id.Client] = id.Clock + 1
	d.log[id.Client] = append(d.log[id.Client], op)
	d.opCount++

	s := d.text()
	s.hintItem = s.start
	s.hintIdx = 0

}

func (d *Doc) VisibleLen() int {
	return d.text().n
}

func (d *Doc) OpCount() int {
	return d.opCount
}

func (d *Doc) StateVector() map[ClientID]uint64 {
	return maps.Clone(d.sv)
}

func (d *Doc) Diff(remote map[ClientID]uint64) []Op {
	var out []Op

	for client, v := range d.log {
		from := remote[client]
		if from >= uint64(len(v)) {
			continue
		}

		out = append(out, v[from:]...)
	}

	return out

}

func (d *Doc) PendingLen() int {
	return len(d.pending)
}

func (d *Doc) DropPending() int {
	br := len(d.pending)
	clear(d.pending)
	d.pending = d.pending[:0]
	return br

}

func (d *Doc) ResumeClock() {
	d.clock = d.sv[d.Client]
}

func (d *Doc) Len() int {

	return len(d.items)

}

func (d *Doc) lookup(p Parent) *container {
	if d.containers == nil {
		return nil
	}
	return d.containers[p]
}

func (d *Doc) container(p Parent) *container {
	if d.containers[p] != nil {
		return d.containers[p]
	}

	if p.IsRoot() {
		cnt := &container{kind: p.RootKind, list: newSeq()}
		d.containers[p] = cnt
		return cnt
	}

	return nil
}

func (d *Doc) seqFor(c *container, key string) *seq {
	if key == "" {
		return c.list
	}

	if c.keys == nil {
		c.keys = make(map[string]*seq)
	}

	if c.keys[key] != nil {
		return c.keys[key]
	} else {
		c.keys[key] = newSeq()
		return c.keys[key]
	}
}

func (d *Doc) kindOf(p Parent) (Kind, bool) {
	if p.IsRoot() {
		return p.RootKind, true
	}

	item := d.items[p.Item]

	if item == nil {
		return 0, false
	}

	if item.CKind != ContentType {
		return 0, false
	}

	return item.Type, true

}

func (d *Doc) ParentOf(p Parent) (Parent, bool) {
	if p.IsRoot() {
		return Parent{}, false
	}

	item := d.items[p.Item]

	if item == nil {
		return Parent{}, false
	}

	return item.Parent, true
}

func (d *Doc) ChangedParents(ops []Op) []Parent {
	var out []Parent
	seen := make(map[Parent]bool)
	add := func(p Parent) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, op := range ops {
		switch o := op.(type) {
		case InsertOp:
			add(o.Parent)
		case DeleteOp:
			if it, ok := d.items[o.Target]; ok {
				add(it.Parent)
			}
		}
	}
	return out
}
