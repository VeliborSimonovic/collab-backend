//go:build js

package main

import (
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

	return obj
}

func main() {
	yata := js.Global().Get("Object").New()
	yata.Set("newDoc", js.FuncOf(newDoc))
	js.Global().Set("yata", yata)
	select {}
}
