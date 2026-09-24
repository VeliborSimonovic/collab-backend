package crdt

import (
	"maps"
	"strings"
)

type Doc struct {
	Client     ClientID
	clock      uint64
	items      map[ID]*Item
	start, end *Item
	sv         map[ClientID]uint64
	log        map[ClientID][]Op
	pending    []Op
	hintItem   *Item
	hintIdx    int
	itemCount  int
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

	start := &Item{ID: StartID}
	end := &Item{ID: EndID}

	start.right = end
	end.left = start

	d := &Doc{
		Client: client,
		items:  make(map[ID]*Item),
		sv:     make(map[ClientID]uint64),
		log:    make(map[ClientID][]Op),
		start:  start,
		end:    end,
	}

	d.items[StartID] = start
	d.items[EndID] = end
	d.hintItem = d.start
	d.hintIdx = 0

	return d
}

func (d *Doc) LocalInsert(pos int, r rune) (InsertOp, error) {

	if pos < 0 {
		return InsertOp{}, ErrOutOfRange
	}

	origin := d.start

	if pos > 0 {
		origin = d.visibleAt(pos - 1)
		if origin == nil {
			return InsertOp{}, ErrOutOfRange
		}
	}

	right := origin.right

	op := InsertOp{
		ID:          ID{Client: d.Client, Clock: d.clock},
		Origin:      origin.ID,
		RightOrigin: right.ID,
		Content:     r,
	}
	d.clock++

	d.apply(op)

	d.hintItem = d.items[op.ID]
	d.hintIdx = pos
	return op, nil

}

func (d *Doc) LocalDelete(pos int) (DeleteOp, error) {
	if pos < 0 {
		return DeleteOp{}, ErrOutOfRange
	}

	target := d.visibleAt(pos)

	if target == nil {
		return DeleteOp{}, ErrOutOfRange
	}

	op := DeleteOp{
		ID:     ID{Client: d.Client, Clock: d.clock},
		Target: target.ID,
	}

	d.clock++

	d.apply(op)

	d.hintItem = d.items[op.Target]
	d.hintIdx = pos
	return op, nil
}

func (d *Doc) String() string {
	var b strings.Builder

	for it := d.start.right; it != d.end; it = it.right {
		if !it.Deleted {
			b.WriteRune(it.Content)
		}

	}

	return b.String()

}

func (d *Doc) Order() []ID {

	arr := make([]ID, 0, len(d.items)-2)

	for it := d.start.right; it != d.end; it = it.right {
		arr = append(arr, it.ID)
	}

	return arr

}

func (d *Doc) visibleAt(k int) *Item {
	if k >= d.hintIdx {
		item := d.hintItem
		idx := d.hintIdx
		for item != d.end {
			visible := !item.Deleted && item != d.start
			if visible && idx == k {
				return item
			}
			if visible {
				idx++
			}
			item = item.right
		}
		return nil
	}

	item := d.hintItem
	idx := d.hintIdx
	for item != d.start {
		item = item.left
		visible := !item.Deleted && item != d.start
		if visible {
			idx--
		}
		if visible && idx == k {
			return item
		}
	}
	return nil
}

func (d *Doc) integrate(item *Item) {

	var left = d.items[item.Origin]
	var right = d.items[item.RightOrigin]

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

	for o = left.right; o != right && o != d.end; o = o.right {
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
		if d.items[o.Origin] == nil || d.items[o.RightOrigin] == nil {
			return notReady
		}
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
		d.integrate(&Item{
			ID:          v.ID,
			Origin:      v.Origin,
			RightOrigin: v.RightOrigin,
			Content:     v.Content,
		})
		d.itemCount++
	case DeleteOp:
		if it := d.items[v.Target]; it != nil {
			it.Deleted = true
		}

	default:
		return

	}

	id := op.OpID()
	d.sv[id.Client] = id.Clock + 1
	d.log[id.Client] = append(d.log[id.Client], op)
	d.hintItem = d.start
	d.hintIdx = 0

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
	return d.itemCount

}
