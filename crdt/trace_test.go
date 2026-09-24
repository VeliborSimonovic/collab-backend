package crdt

import (
	"compress/gzip"
	"encoding/json"
	"math/rand"
	"os"
	"runtime"
	"testing"
	"time"
)

type patch struct {
	position     int
	numDeleted   int
	insertedText string
}

func (p *patch) UnmarshalJSON(b []byte) error {
	var raw [3]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(raw[0], &p.position); err != nil {
		return err
	}
	if err := json.Unmarshal(raw[1], &p.numDeleted); err != nil {
		return err
	}
	return json.Unmarshal(raw[2], &p.insertedText)
}

type txn struct {
	Patches []patch `json:"patches"`
}

type traceFile struct {
	StartContent string `json:"startContent"`
	EndContent   string `json:"endContent"`
	Txns         []txn  `json:"txns"`
}

func loadTrace(path string) (*traceFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	var tf traceFile
	if err := json.NewDecoder(gz).Decode(&tf); err != nil {
		return nil, err
	}

	return &tf, nil
}

func flattenPatches(tf *traceFile) []patch {
	var out []patch
	for _, tx := range tf.Txns {
		out = append(out, tx.Patches...)
	}
	return out
}

func replay(doc *Doc, patches []patch) {
	for _, p := range patches {
		for i := 0; i < p.numDeleted; i++ {
			if _, err := doc.LocalDelete(p.position); err != nil {
				panic(err)
			}

		}

		for i, r := range []rune(p.insertedText) {
			doc.LocalInsert(p.position+i, r)
		}

	}
}

func TestTrace(t *testing.T) {
	path := "testdata/automerge-paper.json.gz"
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("trace file not present")
	}

	tf, err := loadTrace(path)
	if err != nil {
		t.Fatalf("loadTrace: %v", err)
	}
	patches := flattenPatches(tf)

	doc := NewDoc(1)

	start := time.Now()
	replay(doc, patches)
	elapsed := time.Since(start)

	if doc.String() != tf.EndContent {
		t.Fatalf("replayed text does not match endContent")
	}

	runtime.GC()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	t.Logf("replay: %v, heap: %.1f MB, items (incl. tombstones): %d",
		elapsed, float64(mem.HeapAlloc)/1e6, len(doc.Order()))
}

func TestSyntheticScale(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	const n = 10_000_000
	const jumpEvery = 50_000
	doc := NewDoc(1)
	rng := rand.New(rand.NewSource(1))
	letters := []rune("abcdefghijklmnopqrstuvwxyz \n")

	length := 0
	cursor := 0
	inserts, deletes := 0, 0

	start := time.Now()
	for i := 0; i < n; i++ {
		jump := i%jumpEvery == 0

		switch {
		case length == 0 || rng.Float64() < 0.75:
			pos := cursor
			if jump {
				pos = rng.Intn(length + 1)
			}
			if _, err := doc.LocalInsert(pos, letters[rng.Intn(len(letters))]); err != nil {
				t.Fatalf("LocalInsert at %d (len %d): %v", pos, length, err)
			}
			cursor = pos + 1
			length++
			inserts++

		default:
			pos := cursor - 1
			if pos < 0 || jump {
				pos = rng.Intn(length)
			}
			if _, err := doc.LocalDelete(pos); err != nil {
				t.Fatalf("LocalDelete at %d (len %d): %v", pos, length, err)
			}
			cursor = pos
			length--
			deletes++
		}
	}
	elapsed := time.Since(start)

	runtime.GC()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	t.Logf("synthetic %d ops (%d inserts, %d deletes): %v, heap: %.1f MB, items incl. tombstones: %d, visible chars: %d",
		n, inserts, deletes, elapsed, float64(mem.HeapAlloc)/1e6, len(doc.Order()), length)
}
