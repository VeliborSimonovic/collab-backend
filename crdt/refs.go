package crdt

import (
	"errors"
	"strconv"
	"strings"
)

var ErrBadRef = errors.New("crdt: bad ref")

func FormatRef(p Parent) string {
	if p.IsRoot() {
		return "r:" + p.RootKind.String() + ":" + p.Root

	}

	return "i:" + strconv.FormatUint(uint64(p.Item.Client), 10) + ":" + strconv.FormatUint(p.Item.Clock, 10)
}

func ParseRef(s string) (Parent, error) {
	n := strings.SplitN(s, ":", 3)

	if len(n) < 3 {
		return Parent{}, ErrBadRef
	}

	p1 := n[0]
	p2 := n[1]
	p3 := n[2]

	switch p1 {
	case "r":
		var kind Kind
		switch p2 {
		case "text":
			kind = KindText
		case "array":
			kind = KindArray
		case "map":
			kind = KindMap
		default:
			return Parent{}, ErrBadRef
		}

		return RootParent(n[2], kind), nil

	case "i":
		client, err := strconv.ParseUint(p2, 10, 64)

		if err != nil || client == 0 {
			return Parent{}, ErrBadRef
		}

		clock, err := strconv.ParseUint(p3, 10, 64)
		if err != nil {
			return Parent{}, ErrBadRef
		}

		return ItemParent(ID{Client: ClientID(client), Clock: clock}), nil
	}

	return Parent{}, ErrBadRef

}
