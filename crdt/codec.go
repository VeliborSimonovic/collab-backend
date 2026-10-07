package crdt

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
)

var ErrBadMessage = errors.New("crdt: bad message")
var ErrOutOfRange = errors.New("crdt: out of range")

const (
	tagInsert = 1
	tagDelete = 2
	tag3      = 3
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
			if isLegacy(o) {
				buf = append(buf, tagInsert)
				buf = appendID(buf, o.ID)
				buf = appendID(buf, o.Origin)
				buf = appendID(buf, o.RightOrigin)
				buf = binary.AppendUvarint(buf, uint64(uint32(o.Content)))

			} else {
				buf = appendInsert3(buf, o)
			}
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

func (r *reader) bytes(max int) []byte {
	n := r.uvarint()
	if r.err != nil {
		return nil
	}
	if max < 0 || n > uint64(max) || n > uint64(len(r.buf)) {
		r.err = ErrBadMessage
		return nil
	}
	b := bytes.Clone(r.buf[:int(n)])
	r.buf = r.buf[int(n):]
	return b
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

		case tag3:
			op := InsertOp{ID: r.id(), Origin: r.id(), RightOrigin: r.id()}

			switch flag := r.uvarint(); flag {
			case 0:
				name := r.bytes(255)
				kind := r.uvarint()
				if kind > uint64(KindMap) {
					r.err = ErrBadMessage
				}
				op.Parent = RootParent(string(name), Kind(kind))
			case 1:
				item := r.id()
				if item.Client == 0 {
					r.err = ErrBadMessage
				}
				op.Parent = Parent{Item: item}
			default:
				r.err = ErrBadMessage
			}

			op.Key = string(r.bytes(255))

			ckind := r.uvarint()
			if ckind > uint64(ContentType) {
				r.err = ErrBadMessage
			}
			op.CKind = ContentKind(ckind)

			switch op.CKind {
			case ContentRune:
				c := r.uvarint()
				if c > math.MaxInt32 {
					r.err = ErrBadMessage
				}
				op.Content = rune(c)
			case ContentJSON:
				op.JSON = r.bytes(64 << 10)
			case ContentType:
				t := r.uvarint()
				if t > uint64(KindMap) {
					r.err = ErrBadMessage
				}
				op.Type = Kind(t)
			}

			ops = append(ops, op)

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

func isLegacy(op InsertOp) bool {
	if (op.Parent == Parent{} && op.Key == "" && op.CKind == ContentRune) {
		return true
	}

	return false
}

func appendBytes(buf, b []byte) []byte {
	buf = binary.AppendUvarint(buf, uint64(len(b)))
	buf = append(buf, b...)

	return buf
}

func appendInsert3(buf []byte, op InsertOp) []byte {
	buf = binary.AppendUvarint(buf, tag3)
	buf = binary.AppendUvarint(buf, uint64(op.ID.Client))
	buf = binary.AppendUvarint(buf, uint64(op.ID.Clock))
	buf = binary.AppendUvarint(buf, uint64(op.Origin.Client))
	buf = binary.AppendUvarint(buf, uint64(op.Origin.Clock))
	buf = binary.AppendUvarint(buf, uint64(op.RightOrigin.Client))
	buf = binary.AppendUvarint(buf, uint64(op.RightOrigin.Clock))

	if op.Parent.IsRoot() {
		buf = binary.AppendUvarint(buf, 0)
		buf = appendBytes(buf, []byte(op.Parent.Root))
		buf = binary.AppendUvarint(buf, uint64(op.Parent.RootKind))
	} else {
		buf = binary.AppendUvarint(buf, 1)
		buf = binary.AppendUvarint(buf, uint64(op.Parent.Item.Client))
		buf = binary.AppendUvarint(buf, uint64(op.Parent.Item.Clock))
	}

	buf = appendBytes(buf, []byte(op.Key))
	buf = binary.AppendUvarint(buf, uint64(op.CKind))
	switch op.CKind {
	case ContentRune:
		buf = binary.AppendUvarint(buf, uint64(op.Content))
	case ContentJSON:
		buf = appendBytes(buf, op.JSON)
	case ContentType:
		buf = binary.AppendUvarint(buf, uint64(op.Type))
	}
	return buf
}
