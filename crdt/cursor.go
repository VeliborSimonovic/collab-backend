package crdt

func (d *Doc) CursorID(pos int) (ID, error) {
	return d.CursorIDIn(DefaultText(), pos)
}

func (d *Doc) CursorPos(id ID) (pos int, ok bool) {
	return d.CursorPosIn(DefaultText(), id)
}

func (d *Doc) CursorIDIn(p Parent, pos int) (ID, error) {
	if pos < 0 {
		return ID{}, ErrOutOfRange
	}

	s, err := d.seqOf(p, KindText)
	if err != nil {
		s, err = d.seqOf(p, KindArray)
		if err != nil {
			return ID{}, ErrWrongKind
		}
	}

	if pos == 0 {
		return s.start.ID, nil
	}

	count := 0

	for tren := s.start.right; tren != s.end; tren = tren.right {
		if tren.Deleted {
			continue
		}

		count++

		if count == pos {
			return tren.ID, nil
		}
	}

	return ID{}, ErrOutOfRange
}

func (d *Doc) CursorPosIn(p Parent, id ID) (pos int, ok bool) {
	if id == StartID {
		return 0, true
	}

	it := d.items[id]
	if it == nil || id == EndID || it.Parent != p || it.Key != "" {
		return 0, false
	}

	s, err := d.seqOf(p, KindText)
	if err != nil {
		s, err = d.seqOf(p, KindArray)
		if err != nil {
			return 0, false
		}
	}

	count := 0
	for tren := s.start.right; tren != s.end; tren = tren.right {
		if tren.ID == id {
			if tren.Deleted {
				return count, true
			}
			return count + 1, true
		}

		if tren.Deleted {
			continue
		}

		count++
	}

	return 0, false
}
