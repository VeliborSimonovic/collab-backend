package crdt

import (
	"fmt"
	"strings"
)

func (d *Doc) Dump() string {
	var b strings.Builder
	for it := d.start.right; it != d.end; it = it.right {
		mark := " "
		if it.Deleted {
			mark = "x"
		}
		fmt.Fprintf(&b, "%s %d:%d %q  origin=%d:%d  right=%d:%d\n",
			mark, it.ID.Client, it.ID.Clock, it.Content,
			it.Origin.Client, it.Origin.Clock, it.RightOrigin.Client, it.RightOrigin.Clock)
	}
	return b.String()
}

func (d *Doc) Check() error {
	var fwd []*Item
	for it := d.start; it != nil; it = it.right {
		fwd = append(fwd, it)
		if len(fwd) > len(d.items)+5 {
			return fmt.Errorf("forward walk does not end (cycle?)")
		}
	}
	if len(fwd) == 0 || fwd[0] != d.start || fwd[len(fwd)-1] != d.end {
		return fmt.Errorf("forward walk must run from start to end")
	}

	i := len(fwd) - 1
	for it := d.end; it != nil; it = it.left {
		if i < 0 || fwd[i] != it {
			return fmt.Errorf("forward and backward walks disagree: broken left/right pointers")
		}
		i--
	}
	if i != -1 {
		return fmt.Errorf("backward walk is shorter than the forward walk")
	}

	if d.start.left != nil || d.end.right != nil {
		return fmt.Errorf("start.left and end.right must be nil")
	}
	if len(d.items) != len(fwd) {
		return fmt.Errorf("items map has %d entries but the list has %d", len(d.items), len(fwd))
	}

	index := make(map[ID]int, len(fwd))
	for n, it := range fwd {
		if d.items[it.ID] != it {
			return fmt.Errorf("item %v is in the list but not registered in items", it.ID)
		}
		index[it.ID] = n
	}
	for _, it := range fwd {
		if it == d.start || it == d.end {
			continue
		}
		o, ok1 := index[it.Origin]
		r, ok2 := index[it.RightOrigin]
		if !ok1 || !ok2 {
			return fmt.Errorf("item %v names an Origin or RightOrigin that does not exist", it.ID)
		}
		if !(o < index[it.ID] && index[it.ID] < r) {
			return fmt.Errorf("item %v sits outside its origin..rightOrigin range", it.ID)
		}
	}
	return nil
}
