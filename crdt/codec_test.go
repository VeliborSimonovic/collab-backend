package crdt

import (
	"maps"
	"math"
	"math/rand"
	"testing"
)

func TestCodecRoundTrip(t *testing.T) {
	var ops []Op
	for seed := int64(0); len(ops) < 300000; seed++ {
		for _, r := range simulate(seed, false) {
			ops = append(ops, r.doc.Diff(nil)...)
		}
	}
	ops = ops[:300000]

	enc := EncodeOps(ops)
	t.Logf("%d ops -> %d bytes (%.2f bytes/op)", len(ops), len(enc), float64(len(enc))/float64(len(ops)))

	got, err := DecodeOps(enc)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(ops) {
		t.Fatalf("got %d ops, want %d", len(got), len(ops))
	}
	for i := range ops {
		if got[i] != ops[i] {
			t.Fatalf("op %d: got %+v, want %+v", i, got[i], ops[i])
		}
	}

	sv := map[ClientID]uint64{1: 5, 2: 0, 300: 1 << 40}
	encSV := EncodeSV(sv)
	t.Logf("state vector with %d clients -> %d bytes", len(sv), len(encSV))
	gotSV, err := DecodeSV(encSV)
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(gotSV, sv) {
		t.Fatalf("sv: got %v, want %v", gotSV, sv)
	}

	empty, err := DecodeOps(EncodeOps(nil))
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty: %v %v", empty, err)
	}
}

func TestDecodeGarbage(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 3000000; i++ {
		b := make([]byte, rng.Intn(41))
		rng.Read(b)
		DecodeOps(b)
		DecodeSV(b)
	}
	if _, err := DecodeOps([]byte{0xff, 0xff, 0xff, 0xff, 0x0f}); err == nil {
		t.Fatal("expected error")
	}
}

func TestCodecHugeClientIDs(t *testing.T) {
	big := ClientID(1<<64 - 1)
	ops := []Op{
		InsertOp{ID: ID{big, 0}, Origin: StartID, RightOrigin: EndID, Content: 'a'},
		InsertOp{ID: ID{big, 1}, Origin: ID{big, 0}, RightOrigin: EndID, Content: '😀'},
		DeleteOp{ID: ID{big - 1, 1<<63 + 5}, Target: ID{big, 0}},
	}
	enc := EncodeOps(ops)
	t.Logf("%d ops -> %d bytes", len(ops), len(enc))

	got, err := DecodeOps(enc)
	if err != nil {
		t.Fatal(err)
	}
	for i := range ops {
		if got[i] != ops[i] {
			t.Fatalf("op %d: got %+v, want %+v", i, got[i], ops[i])
		}
	}

	sv := map[ClientID]uint64{big: 1 << 48, ClientID(math.MaxUint64): math.MaxUint64}
	gotSV, err := DecodeSV(EncodeSV(sv))
	if err != nil || !maps.Equal(gotSV, sv) {
		t.Fatalf("sv: got %v, err %v", gotSV, err)
	}
}
