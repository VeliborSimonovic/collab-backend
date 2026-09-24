package crdt

func (d *Doc) CursorID(pos int) (ID, error) {
	if pos < 0 {
		return ID{}, ErrOutOfRange
	}
	if pos == 0 {
		return d.start.ID, nil
	}

	count := 0

	for tren := d.start.right; tren != d.end; tren = tren.right {
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

func (d *Doc) CursorPos(id ID) (pos int, ok bool) {
	if id == StartID {
		return 0, true
	}

	if d.items[id] == nil || id == EndID {
		return 0, false
	}

	count := 0
	for tren := d.start.right; tren != d.end; tren = tren.right {
		if tren.ID == id {
			if tren.Deleted {
				return count, true
			} else {
				return count + 1, true
			}
		}

		if tren.Deleted {
			continue
		}

		count++
	}

	return 0, false

}
