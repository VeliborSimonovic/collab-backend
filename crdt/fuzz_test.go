package crdt

import (
	"bytes"
	"flag"
	"fmt"
	"math/rand"
	"slices"
	"sort"
	"testing"
)

var long = flag.Bool("long", false, "run 100,000 fuzz seeds instead of 5,000")

func fuzzSeeds() int64 {
	if *long {
		return 100000
	}
	return 5000
}

type replica struct {
	doc   *Doc
	inbox []Op
	trace []string
}

func (r *replica) note(kind string, op Op) {
	r.trace = append(r.trace, fmt.Sprintf("%s %+v", kind, op))
}

var fuzzKeys = []string{"a", "b", "c"}

func nestedParents(d *Doc) []Parent {
	var ps []Parent
	for p := range d.containers {
		if p.IsRoot() {
			continue
		}
		if it := d.items[p.Item]; it == nil || it.Deleted {
			continue
		}
		ps = append(ps, p)
	}
	sort.Slice(ps, func(i, j int) bool {
		a, b := ps[i].Item, ps[j].Item
		if a.Client != b.Client {
			return a.Client < b.Client
		}
		return a.Clock < b.Clock
	})
	return ps
}

func editContainer(r *rand.Rand, d *Doc, p Parent, kind Kind) []Op {
	var ops []Op
	switch kind {
	case KindText:
		str, _ := d.ToJSON(p).(string)
		n := len([]rune(str))
		if n == 0 || r.Intn(4) != 0 {
			ops, _ = d.TextInsert(p, r.Intn(n+1), string(rune('a'+r.Intn(26))))
		} else {
			ops, _ = d.TextDelete(p, r.Intn(n), 1)
		}
	case KindArray:
		arr, _ := d.ToJSON(p).([]any)
		n := len(arr)
		switch k := r.Intn(3); {
		case k == 0 && n > 0:
			ops, _ = d.ArrayDelete(p, r.Intn(n), 1)
		case k == 1:
			if op, err := d.ArrayInsertType(p, r.Intn(n+1), KindMap); err == nil {
				ops = []Op{op}
			}
		default:
			if op, err := d.ArrayInsertJSON(p, r.Intn(n+1), []byte(fmt.Sprint(r.Intn(100)))); err == nil {
				ops = []Op{op}
			}
		}
	case KindMap:
		key := fuzzKeys[r.Intn(len(fuzzKeys))]
		switch r.Intn(3) {
		case 0:
			if op, err := d.MapDelete(p, key); err == nil {
				ops = []Op{op}
			}
		case 1:
			if op, err := d.MapSetType(p, key, KindArray); err == nil {
				ops = []Op{op}
			}
		default:
			if op, err := d.MapSetJSON(p, key, []byte(fmt.Sprint(r.Intn(100)))); err == nil {
				ops = []Op{op}
			}
		}
	}
	return ops
}

func randomShared(r *rand.Rand, d *Doc) []Op {
	x := r.Intn(100)
	switch {
	case x < 30:
		return editContainer(r, d, DefaultText(), KindText)
	case x < 50:
		return editContainer(r, d, RootParent("a", KindArray), KindArray)
	case x < 70:
		return editContainer(r, d, RootParent("m", KindMap), KindMap)
	}
	ps := nestedParents(d)
	if len(ps) == 0 {
		return nil
	}
	p := ps[r.Intn(len(ps))]
	return editContainer(r, d, p, d.containers[p].kind)
}

func simulate(seed int64, lossy bool) []*replica {
	rng := rand.New(rand.NewSource(seed))

	n := 3 + rng.Intn(8)
	reps := make([]*replica, n)
	for i := range reps {
		reps[i] = &replica{doc: NewDoc(ClientID(i + 1))}
	}

	steps := 20 + rng.Intn(400)
	for s := 0; s < steps; s++ {
		switch {
		case lossy && rng.Intn(20) == 0:
			reps[rng.Intn(n)].inbox = nil

		case rng.Intn(2) == 0:
			i := rng.Intn(n)
			ops := randomShared(rng, reps[i].doc)
			for _, op := range ops {
				reps[i].note("local", op)
				for j, other := range reps {
					if j != i {
						other.inbox = append(other.inbox, op)
					}
				}
			}

		default:
			r := reps[rng.Intn(n)]
			if len(r.inbox) == 0 {
				continue
			}
			k := rng.Intn(len(r.inbox))
			op := r.inbox[k]
			r.note("recv", op)
			r.doc.Receive(op)

			if rng.Intn(10) == 0 {
				continue
			}
			r.inbox = append(r.inbox[:k], r.inbox[k+1:]...)
		}
	}

	for _, r := range reps {
		r.doc.Receive(r.inbox...)
		r.inbox = nil
	}

	if lossy {
		for pass := 0; pass < 2; pass++ {
			for i := range reps {
				for j := range reps {
					if i == j {
						continue
					}
					reps[j].doc.Receive(reps[i].doc.Diff(reps[j].doc.StateVector())...)
				}
			}
		}
	}

	return reps
}

func countVisible(s *seq) int {
	n := 0
	for it := s.start.right; it != s.end; it = it.right {
		if !it.Deleted {
			n++
		}
	}
	return n
}

func assertConverged(t *testing.T, seed int64, reps []*replica) {
	t.Helper()
	defer func() {
		if t.Failed() {
			for i, r := range reps {
				t.Logf("seed %d replica %d trace:", seed, i)
				for _, l := range r.trace {
					t.Log("  " + l)
				}
			}
		}
	}()

	wantJSON, _ := reps[0].doc.JSON()
	for i, r := range reps {
		got, _ := r.doc.JSON()
		if !bytes.Equal(got, wantJSON) {
			t.Fatalf("seed %d: replica 0 JSON %s, replica %d JSON %s", seed, wantJSON, i, got)
		}
		for p, c := range r.doc.containers {
			ss := []*seq{c.list}
			for _, s := range c.keys {
				ss = append(ss, s)
			}
			for _, s := range ss {
				if s != nil && s.n != countVisible(s) {
					t.Fatalf("seed %d: replica %d container %v: n=%d, walk=%d", seed, i, p, s.n, countVisible(s))
				}
			}
		}
	}

	wantText := reps[0].doc.String()
	wantOrder := reps[0].doc.Order()

	for i, r := range reps {
		if got := r.doc.String(); got != wantText {
			t.Fatalf("seed %d: replica 0 has %q, replica %d has %q", seed, wantText, i, got)
		}
		if got := r.doc.Order(); !slices.Equal(got, wantOrder) {
			t.Fatalf("seed %d: replica %d has a different item order (text still matches, so this is a latent divergence)", seed, i)
		}
		if n := r.doc.PendingLen(); n != 0 {
			t.Fatalf("seed %d: replica %d still has %d ops pending", seed, i, n)
		}
	}

	index := make(map[ID]int, len(wantOrder)+2)
	index[StartID] = -1
	index[EndID] = len(wantOrder)
	for n, id := range wantOrder {
		index[id] = n
	}
	for n, id := range wantOrder {
		it := reps[0].doc.items[id]
		if it == nil {
			t.Fatalf("seed %d: item %v is in Order() but not in items", seed, id)
		}
		o, ok1 := index[it.Origin]
		r, ok2 := index[it.RightOrigin]
		if !ok1 || !ok2 {
			t.Fatalf("seed %d: item %v names an Origin or RightOrigin that is not in the document", seed, id)
		}
		if !(o < n && n < r) {
			t.Fatalf("seed %d: item %v sits at %d, outside its origin(%d)..rightOrigin(%d) range",
				seed, id, n, o, r)
		}
	}
}

func TestFuzzConverges(t *testing.T) {
	for seed := int64(0); seed < fuzzSeeds(); seed++ {
		assertConverged(t, seed, simulate(seed, false))
	}
}

func TestFuzzLossyDiffSync(t *testing.T) {
	for seed := int64(0); seed < fuzzSeeds(); seed++ {
		assertConverged(t, seed, simulate(seed, true))
	}
}
