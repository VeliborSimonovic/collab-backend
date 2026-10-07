package crdt

type seq struct {
	start    *Item
	end      *Item
	n        int
	hintItem *Item
	hintIdx  int
}

func newSeq() *seq {
	start := &Item{ID: StartID}
	end := &Item{ID: EndID}
	start.right = end
	end.left = start

	return &seq{start: start, end: end, n: 0, hintItem: start, hintIdx: 0}
}

func (s *seq) visibleCount() int {

	return s.n
}

func (s *seq) visibleAt(k int) *Item {
	if k >= s.hintIdx {
		item := s.hintItem
		idx := s.hintIdx
		for item != s.end {
			visible := !item.Deleted && item != s.start
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

	item := s.hintItem
	idx := s.hintIdx
	for item != s.start {
		item = item.left
		visible := !item.Deleted && item != s.start
		if visible {
			idx--
		}
		if visible && idx == k {
			return item
		}
	}
	return nil
}

func (s *seq) last() *Item {
	return s.end.left
}

type container struct {
	kind Kind
	list *seq
	keys map[string]*seq
}

func (c *container) isEmpty() bool {

	if c.list != nil && c.list.start.right != c.list.end {
		return false
	}

	for _, s := range c.keys {
		if s.start.right != s.end {
			return false
		}
	}

	return true
}
