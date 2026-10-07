package crdt

import (
	"maps"
	"math"
	"math/rand"
	"reflect"
	"testing"
)

func TestCodecRoundTrip(t *testing.T) {
	var ops []Op
	for seed := int64(0); len(ops) < 300000; seed++ {
		for _, r := range simulate(seed, false) {
			for _, op := range r.doc.Diff(nil) {
				// the wire format does not carry containers or JSON yet
				if in, ok := op.(InsertOp); ok && (in.Parent != DefaultText() || in.CKind != ContentRune) {
					continue
				}
				ops = append(ops, op)
			}
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
		if !reflect.DeepEqual(got[i], ops[i]) {
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

	var shared []Op
	for seed := int64(0); len(shared) < 500; seed++ {
		rng := rand.New(rand.NewSource(seed))
		d := NewDoc(ClientID(seed + 1))
		for i := 0; i < 200 && len(shared) < 500; i++ {
			shared = append(shared, randomShared(rng, d)...)
		}
	}
	shared = shared[:500]

	gotShared, err := DecodeOps(EncodeOps(shared))
	if err != nil {
		t.Fatal(err)
	}
	if len(gotShared) != len(shared) {
		t.Fatalf("shared: got %d ops, want %d", len(gotShared), len(shared))
	}
	for i := range shared {
		if !reflect.DeepEqual(gotShared[i], shared[i]) {
			t.Fatalf("shared op %d: got %+v, want %+v", i, gotShared[i], shared[i])
		}
	}
}

func TestLegacyStaysTag1(t *testing.T) {
	d := NewDoc(1)
	op, err := d.LocalInsert(0, 'a')
	if err != nil {
		t.Fatal(err)
	}
	enc := EncodeOps([]Op{op})
	if len(enc) < 2 || enc[0] != 1 || enc[1] != tagInsert {
		t.Fatalf("got prefix %v, want count 1 then tag 1", enc[:min(2, len(enc))])
	}
}

func TestDecodeGarbage(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 3000000; i++ {
		b := make([]byte, rng.Intn(41))
		rng.Read(b)
		if i%2 == 0 && len(b) >= 2 {
			b[0], b[1] = 1, tag3
		}
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
		if !reflect.DeepEqual(got[i], ops[i]) {
			t.Fatalf("op %d: got %+v, want %+v", i, got[i], ops[i])
		}
	}

	sv := map[ClientID]uint64{big: 1 << 48, ClientID(math.MaxUint64): math.MaxUint64}
	gotSV, err := DecodeSV(EncodeSV(sv))
	if err != nil || !maps.Equal(gotSV, sv) {
		t.Fatalf("sv: got %v, err %v", gotSV, err)
	}
}
