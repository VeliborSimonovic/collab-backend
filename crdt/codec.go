package crdt

import (
	"encoding/binary"
	"errors"
	"math"
)

var ErrBadMessage = errors.New("crdt: bad message")
var ErrOutOfRange = errors.New("crdt: out of range")

const (
	tagInsert = 1
	tagDelete = 2
)

func appendID(buf []byte, id ID) []byte {
	buf = binary.AppendUvarint(buf, uint64(id.Client))
	return binary.AppendUvarint(buf, id.Clock)
}

func EncodeOps(ops []Op) []byte {
	buf := binary.AppendUvarint(nil, uint64(len(ops)))
	for _, op := range ops {
		switch o := op.(type) {
		case InsertOp:
			buf = append(buf, tagInsert)
			buf = appendID(buf, o.ID)
			buf = appendID(buf, o.Origin)
			buf = appendID(buf, o.RightOrigin)
			buf = binary.AppendUvarint(buf, uint64(uint32(o.Content)))
		case DeleteOp:
			buf = append(buf, tagDelete)
			buf = appendID(buf, o.ID)
			buf = appendID(buf, o.Target)
		}
	}
	return buf
}

func EncodeSV(sv map[ClientID]uint64) []byte {
	buf := binary.AppendUvarint(nil, uint64(len(sv)))
	for c, clock := range sv {
		buf = binary.AppendUvarint(buf, uint64(c))
		buf = binary.AppendUvarint(buf, clock)
	}
	return buf
}

type reader struct {
	buf []byte
	err error
}

func (r *reader) uvarint() uint64 {
	if r.err != nil {
		return 0
	}
	n, size := binary.Uvarint(r.buf)
	if size <= 0 {
		r.err = ErrBadMessage
		return 0
	}
	r.buf = r.buf[size:]
	return n
}

func (r *reader) id() ID {
	client := r.uvarint()
	clock := r.uvarint()
	return ID{Client: ClientID(client), Clock: clock}
}

func (r *reader) byte() byte {
	if r.err != nil {
		return 0
	}
	if len(r.buf) == 0 {
		r.err = ErrBadMessage
		return 0
	}
	b := r.buf[0]
	r.buf = r.buf[1:]
	return b
}

func DecodeOps(b []byte) ([]Op, error) {
	r := &reader{buf: b}
	count := r.uvarint()
	if r.err != nil || count > uint64(len(r.buf)) {
		return nil, ErrBadMessage
	}

	ops := make([]Op, 0, count)
	for i := uint64(0); i < count; i++ {
		switch tag := r.byte(); tag {
		case tagInsert:
			op := InsertOp{ID: r.id(), Origin: r.id(), RightOrigin: r.id()}
			c := r.uvarint()
			if c > math.MaxInt32 {
				r.err = ErrBadMessage
			}
			op.Content = rune(c)
			ops = append(ops, op)
		case tagDelete:
			ops = append(ops, DeleteOp{ID: r.id(), Target: r.id()})
		default:
			return nil, ErrBadMessage
		}
		if r.err != nil {
			return nil, ErrBadMessage
		}
	}
	if len(r.buf) != 0 {
		return nil, ErrBadMessage
	}
	return ops, nil
}

func DecodeSV(b []byte) (map[ClientID]uint64, error) {
	r := &reader{buf: b}
	count := r.uvarint()
	if r.err != nil || count > uint64(len(r.buf)) {
		return nil, ErrBadMessage
	}

	sv := make(map[ClientID]uint64, count)
	for i := uint64(0); i < count; i++ {
		c := r.uvarint()
		clock := r.uvarint()
		if r.err != nil {
			return nil, ErrBadMessage
		}
		sv[ClientID(c)] = clock
	}
	if len(r.buf) != 0 {
		return nil, ErrBadMessage
	}
	return sv, nil
}
