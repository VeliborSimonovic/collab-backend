package crdt

import (
	"math/rand"
	"slices"
	"testing"
)

type replica struct {
	doc   *Doc
	inbox []Op
}

func simulate(seed int64, lossy bool) []*replica {
	rng := rand.New(rand.NewSource(seed))

	n := 3 + rng.Intn(8)
	reps := make([]*replica, n)
	for i := range reps {
		reps[i] = &replica{doc: NewDoc(ClientID(i + 1))}
	}

	hot := 0
	spread := 1 + rng.Intn(4)

	steps := 20 + rng.Intn(400)
	for s := 0; s < steps; s++ {
		switch {
		case lossy && rng.Intn(20) == 0:
			reps[rng.Intn(n)].inbox = nil

		case rng.Intn(2) == 0:
			i := rng.Intn(n)
			r := reps[i]
			length := len([]rune(r.doc.String()))

			pos := hot + rng.Intn(2*spread+1) - spread
			pos = max(0, min(pos, length))
			if rng.Intn(10) == 0 {
				hot = rng.Intn(length + 1)
			}

			var op Op
			if length == 0 || rng.Intn(4) != 0 {
				o, err := r.doc.LocalInsert(pos, rune('a'+rng.Intn(26)))
				if err != nil {
					continue
				}
				op = o
			} else {
				o, err := r.doc.LocalDelete(min(pos, length-1))
				if err != nil {
					continue
				}
				op = o
			}
			for j, other := range reps {
				if j != i {
					other.inbox = append(other.inbox, op)
				}
			}

		default:
			r := reps[rng.Intn(n)]
			if len(r.inbox) == 0 {
				continue
			}
			k := rng.Intn(len(r.inbox))
			op := r.inbox[k]
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

func assertConverged(t *testing.T, seed int64, reps []*replica) {
	t.Helper()

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
	for seed := int64(0); seed < 3000; seed++ {
		assertConverged(t, seed, simulate(seed, false))
	}
}

func TestFuzzLossyDiffSync(t *testing.T) {
	for seed := int64(0); seed < 3000; seed++ {
		assertConverged(t, seed, simulate(seed, true))
	}
}
