//go:build js

package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/veliborsimonovic/collab/crdt"
)

func bytesToGo(v js.Value) []byte {
	dst := make([]byte, v.Get("length").Int())
	js.CopyBytesToGo(dst, v)
	return dst
}

func bytesToJS(b []byte) js.Value {
	arr := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(arr, b)
	return arr
}

func newDoc(this js.Value, args []js.Value) any {
	doc := crdt.NewDoc(crdt.ClientID(args[0].Float()))
	obj := js.Global().Get("Object").New()

	obj.Set("insert", js.FuncOf(func(this js.Value, args []js.Value) any {
		pos := args[0].Int()
		var ops []crdt.Op
		for i, r := range []rune(args[1].String()) {
			op, err := doc.LocalInsert(pos+i, r)
			if err != nil {
				break
			}
			ops = append(ops, op)
		}
		return bytesToJS(crdt.EncodeOps(ops))
	}))

	obj.Set("del", js.FuncOf(func(this js.Value, args []js.Value) any {
		pos, count := args[0].Int(), args[1].Int()
		var ops []crdt.Op
		for i := 0; i < count; i++ {
			op, err := doc.LocalDelete(pos)
			if err != nil {
				break
			}
			ops = append(ops, op)
		}
		return bytesToJS(crdt.EncodeOps(ops))
	}))

	obj.Set("apply", js.FuncOf(func(this js.Value, args []js.Value) any {
		ops, err := crdt.DecodeOps(bytesToGo(args[0]))
		if err != nil {
			return err.Error()
		}
		doc.Receive(ops...)
		return nil
	}))

	obj.Set("text", js.FuncOf(func(this js.Value, args []js.Value) any {
		return doc.String()
	}))

	obj.Set("stateVector", js.FuncOf(func(this js.Value, args []js.Value) any {
		return bytesToJS(crdt.EncodeSV(doc.StateVector()))
	}))

	obj.Set("diff", js.FuncOf(func(this js.Value, args []js.Value) any {
		sv, err := crdt.DecodeSV(bytesToGo(args[0]))
		if err != nil {
			return bytesToJS(crdt.EncodeOps(nil))
		}
		return bytesToJS(crdt.EncodeOps(doc.Diff(sv)))
	}))

	obj.Set("cursorId", js.FuncOf(func(this js.Value, args []js.Value) any {
		id, err := doc.CursorID(args[0].Int())
		if err != nil {
			return nil
		}
		return js.ValueOf([]any{float64(id.Client), float64(id.Clock)})
	}))

	obj.Set("cursorPos", js.FuncOf(func(this js.Value, args []js.Value) any {
		id := crdt.ID{Client: crdt.ClientID(args[0].Float()), Clock: uint64(args[1].Float())}
		pos, ok := doc.CursorPos(id)
		if !ok {
			return -1
		}
		return pos
	}))

	method := func(name string, f func(args []js.Value) js.Value) {
		obj.Set(name, js.FuncOf(func(this js.Value, args []js.Value) any {
			return f(args)
		}))
	}

	method("textInsert", func(a []js.Value) js.Value {
		p, err := refArg(a[0])
		if err != nil {
			return result(nil, "", err)
		}
		ops, err := doc.TextInsert(p, a[1].Int(), a[2].String())
		return result(ops, "", err)
	})

	method("textDelete", func(a []js.Value) js.Value {
		p, err := refArg(a[0])
		if err != nil {
			return result(nil, "", err)
		}
		ops, err := doc.TextDelete(p, a[1].Int(), a[2].Int())
		return result(ops, "", err)
	})

	method("arrayInsertJSON", func(a []js.Value) js.Value {
		p, err := refArg(a[0])
		if err != nil {
			return result(nil, "", err)
		}
		op, err := doc.ArrayInsertJSON(p, a[1].Int(), []byte(a[2].String()))
		if err != nil {
			return result(nil, "", err)
		}
		return result([]crdt.Op{op}, "", nil)
	})

	method("arrayInsertType", func(a []js.Value) js.Value {
		p, err := refArg(a[0])
		if err != nil {
			return result(nil, "", err)
		}
		kind, err := kindArg(a[2])
		if err != nil {
			return result(nil, "", err)
		}
		op, err := doc.ArrayInsertType(p, a[1].Int(), kind)
		if err != nil {
			return result(nil, "", err)
		}
		return result([]crdt.Op{op}, crdt.FormatRef(crdt.ItemParent(op.ID)), nil)
	})

	method("arrayDelete", func(a []js.Value) js.Value {
		p, err := refArg(a[0])
		if err != nil {
			return result(nil, "", err)
		}
		ops, err := doc.ArrayDelete(p, a[1].Int(), a[2].Int())
		return result(ops, "", err)
	})

	method("mapSetJSON", func(a []js.Value) js.Value {
		p, err := refArg(a[0])
		if err != nil {
			return result(nil, "", err)
		}
		op, err := doc.MapSetJSON(p, a[1].String(), []byte(a[2].String()))
		if err != nil {
			return result(nil, "", err)
		}
		return result([]crdt.Op{op}, "", nil)
	})

	method("mapSetType", func(a []js.Value) js.Value {
		p, err := refArg(a[0])
		if err != nil {
			return result(nil, "", err)
		}
		kind, err := kindArg(a[2])
		if err != nil {
			return result(nil, "", err)
		}
		op, err := doc.MapSetType(p, a[1].String(), kind)
		if err != nil {
			return result(nil, "", err)
		}
		return result([]crdt.Op{op}, crdt.FormatRef(crdt.ItemParent(op.ID)), nil)
	})

	method("mapDelete", func(a []js.Value) js.Value {
		p, err := refArg(a[0])
		if err != nil {
			return result(nil, "", err)
		}
		op, err := doc.MapDelete(p, a[1].String())
		if err != nil {
			return result(nil, "", err)
		}
		return result([]crdt.Op{op}, "", nil)
	})

	method("toJSON", func(a []js.Value) js.Value {
		p, err := refArg(a[0])
		if err != nil {
			return js.ValueOf("null")
		}
		b, err := json.Marshal(doc.ToJSON(p))
		if err != nil {
			return js.ValueOf("null")
		}
		return js.ValueOf(string(b))
	})

	method("parentOf", func(a []js.Value) js.Value {
		p, err := refArg(a[0])
		if err != nil {
			return js.ValueOf("")
		}
		parent, ok := doc.ParentOf(p)
		if !ok {
			return js.ValueOf("")
		}
		return js.ValueOf(crdt.FormatRef(parent))
	})

	method("mapChild", func(a []js.Value) js.Value {
		p, err := refArg(a[0])
		if err != nil {
			return js.ValueOf("")
		}
		child, ok := doc.MapChild(p, a[1].String())
		if !ok {
			return js.ValueOf("")
		}
		return js.ValueOf(crdt.FormatRef(child))
	})

	method("cursorIdIn", func(a []js.Value) js.Value {
		p, err := refArg(a[0])
		if err != nil {
			return js.Null()
		}
		id, err := doc.CursorIDIn(p, a[1].Int())
		if err != nil {
			return js.Null()
		}
		return js.ValueOf([]any{float64(id.Client), float64(id.Clock)})
	})

	method("cursorPosIn", func(a []js.Value) js.Value {
		p, err := refArg(a[0])
		if err != nil {
			return js.ValueOf(-1)
		}
		id := crdt.ID{Client: crdt.ClientID(a[1].Float()), Clock: uint64(a[2].Float())}
		pos, ok := doc.CursorPosIn(p, id)
		if !ok {
			return js.ValueOf(-1)
		}
		return js.ValueOf(pos)
	})

	method("applyTracked", func(a []js.Value) js.Value {
		type tracked struct {
			Error   *string  `json:"error"`
			Changed []string `json:"changed"`
		}
		out := tracked{Changed: []string{}}
		ops, err := crdt.DecodeOps(bytesToGo(a[0]))
		if err != nil {
			msg := err.Error()
			out.Error = &msg
		} else {
			applied := doc.Receive(ops...)
			for _, p := range doc.ChangedParents(applied) {
				out.Changed = append(out.Changed, crdt.FormatRef(p))
			}
		}
		b, _ := json.Marshal(out)
		return js.ValueOf(string(b))
	})

	return obj
}

func refArg(v js.Value) (crdt.Parent, error) {
	return crdt.ParseRef(v.String())
}

const kindInvalid crdt.Kind = 0xFF

func kindArg(v js.Value) (crdt.Kind, error) {
	if v.Type() != js.TypeString {
		return kindInvalid, crdt.ErrWrongKind
	}
	switch v.String() {
	case "text":
		return crdt.KindText, nil
	case "array":
		return crdt.KindArray, nil
	case "map":
		return crdt.KindMap, nil
	}
	return kindInvalid, crdt.ErrWrongKind
}

func result(ops []crdt.Op, ref string, err error) js.Value {
	obj := js.Global().Get("Object").New()
	obj.Set("ops", bytesToJS(crdt.EncodeOps(ops)))
	if ref == "" {
		obj.Set("ref", js.Null())
	} else {
		obj.Set("ref", ref)
	}
	if err == nil {
		obj.Set("error", js.Null())
	} else {
		obj.Set("error", err.Error())
	}
	return obj
}

func main() {
	yata := js.Global().Get("Object").New()
	yata.Set("newDoc", js.FuncOf(newDoc))
	js.Global().Set("yata", yata)
	select {}
}
