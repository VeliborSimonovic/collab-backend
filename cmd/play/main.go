package main

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/veliborsimonovic/collab/crdt"
)

type peer struct {
	name  string
	doc   *crdt.Doc
	queue []crdt.Op
}

var peers = map[string]*peer{}

func main() {
	peers["a"] = &peer{name: "a", doc: crdt.NewDoc(1)}
	peers["b"] = &peer{name: "b", doc: crdt.NewDoc(2)}

	fmt.Println(`commands:
  a i 3 hello     insert "hello" at position 3 on doc a
  a d 2 [n]       delete n characters (default 1) at position 2 on doc a
  send a b        deliver a's queued ops to b, in order
  send a b r      deliver them in REVERSE order (watch the pending buffer)
  sync            full Diff handshake both ways
  st              show both texts + pending counts
  dump a          show a's item list (x = tombstone)
  raw a 3:0 S E l feed doc a a raw op: ID 3:0, origin START, rightOrigin END, 'l'
                  (S = START, E = END, or another op's ID like 3:0)
  case mlq        run counterexample "mlq" in all 6 delivery orders
  case pvc        run counterexample "pvc" in all 6 delivery orders
  check           run the internal invariant checks on both docs
  sv              show both state vectors
  diff a b        show the ops a would send to b right now
  dup             re-deliver the last op that was sent (must apply 0)
  fuzz [n]        n random concurrent edits + random delivery, then sync (default 200)
  q               quit`)

	sc := bufio.NewScanner(os.Stdin)
	prompt()
	for sc.Scan() {
		run(strings.Fields(sc.Text()))
		prompt()
	}
}

func prompt() { fmt.Print("> ") }

func run(f []string) {
	if len(f) == 0 {
		return
	}
	switch f[0] {
	case "q":
		os.Exit(0)
	case "st":
		status()
	case "dump":
		if p := peers[arg(f, 1)]; p != nil {
			fmt.Print(p.doc.Dump())
		}
	case "send":
		send(arg(f, 1), arg(f, 2), arg(f, 3) == "r")
		status()
	case "sync":
		sync()
		status()
	case "raw":
		raw(f)
		status()
	case "case":
		runCase(arg(f, 1))
	case "check":
		check()
	case "sv":
		showSV()
	case "diff":
		showDiff(arg(f, 1), arg(f, 2))
	case "dup":
		dup()
	case "fuzz":
		n := 200
		if v, err := strconv.Atoi(arg(f, 1)); err == nil {
			n = v
		}
		fuzz(n)
	case "a", "b":
		edit(peers[f[0]], f)
		status()
	default:
		fmt.Println("unknown command")
	}
}

func arg(f []string, i int) string {
	if i < len(f) {
		return f[i]
	}
	return ""
}

func edit(p *peer, f []string) {
	if p == nil || len(f) < 3 {
		fmt.Println("usage: a i <pos> <text>  |  a d <pos> [count]")
		return
	}
	pos, err := strconv.Atoi(f[2])
	if err != nil {
		fmt.Println("bad position")
		return
	}
	switch f[1] {
	case "i":
		if len(f) < 4 {
			fmt.Println("usage: a i <pos> <text>")
			return
		}
		for i, r := range []rune(f[3]) {
			op, err := p.doc.LocalInsert(pos+i, r)
			if err != nil {
				fmt.Println("error:", err)
				return
			}
			p.queue = append(p.queue, op)
		}
	case "d":
		n := 1
		if len(f) > 3 {
			n, _ = strconv.Atoi(f[3])
		}
		for i := 0; i < n; i++ {
			op, err := p.doc.LocalDelete(pos)
			if err != nil {
				fmt.Println("error:", err)
				return
			}
			p.queue = append(p.queue, op)
		}
	default:
		fmt.Println("use i or d")
	}
}

func send(from, to string, reverse bool) {
	src, dst := peers[from], peers[to]
	if src == nil || dst == nil {
		fmt.Println("usage: send a b [r]")
		return
	}
	ops := src.queue
	src.queue = nil
	lastSentTo = to
	if reverse {
		for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
			ops[i], ops[j] = ops[j], ops[i]
		}
	}
	for _, op := range ops {
		lastSent = op
		applied := dst.doc.Receive(op)
		fmt.Printf("  %v -> %s: applied %d, pending %d\n",
			op.OpID(), dst.name, len(applied), dst.doc.PendingLen())
	}
}

func sync() {
	a, b := peers["a"].doc, peers["b"].doc
	b.Receive(a.Diff(b.StateVector())...)
	a.Receive(b.Diff(a.StateVector())...)
	peers["a"].queue, peers["b"].queue = nil, nil
}

func parseID(s string) (crdt.ID, error) {
	switch s {
	case "S", "s":
		return crdt.StartID, nil
	case "E", "e":
		return crdt.EndID, nil
	}
	c, k, ok := strings.Cut(s, ":")
	if !ok {
		return crdt.ID{}, fmt.Errorf("want S, E or client:clock, got %q", s)
	}
	client, err1 := strconv.ParseUint(c, 10, 64)
	clock, err2 := strconv.ParseUint(k, 10, 64)
	if err1 != nil || err2 != nil {
		return crdt.ID{}, fmt.Errorf("bad id %q", s)
	}
	return crdt.ID{Client: crdt.ClientID(client), Clock: clock}, nil
}

func raw(f []string) {
	if len(f) < 6 {
		fmt.Println("usage: raw a 3:0 S E l")
		return
	}
	p := peers[f[1]]
	if p == nil {
		fmt.Println("no such doc:", f[1])
		return
	}
	id, err := parseID(f[2])
	if err != nil {
		fmt.Println(err)
		return
	}
	origin, err := parseID(f[3])
	if err != nil {
		fmt.Println(err)
		return
	}
	rightOrigin, err := parseID(f[4])
	if err != nil {
		fmt.Println(err)
		return
	}
	r := []rune(f[5])[0]

	op := crdt.InsertOp{ID: id, Origin: origin, RightOrigin: rightOrigin, Content: r}
	applied := p.doc.Receive(op)
	fmt.Printf("  %v -> %s: applied %d, pending %d\n", id, p.name, len(applied), p.doc.PendingLen())
}

func counterexample(name string) ([]crdt.Op, string, bool) {
	l := crdt.ID{Client: 3, Clock: 0}
	v := crdt.ID{Client: 1, Clock: 0}
	switch name {
	case "mlq":
		return []crdt.Op{
			crdt.InsertOp{ID: l, Origin: crdt.StartID, RightOrigin: crdt.EndID, Content: 'l'},
			crdt.InsertOp{ID: crdt.ID{Client: 3, Clock: 1}, Origin: l, RightOrigin: crdt.EndID, Content: 'q'},
			crdt.InsertOp{ID: crdt.ID{Client: 2, Clock: 0}, Origin: crdt.StartID, RightOrigin: crdt.EndID, Content: 'm'},
		}, "mlq", true
	case "pvc":
		return []crdt.Op{
			crdt.InsertOp{ID: v, Origin: crdt.StartID, RightOrigin: crdt.EndID, Content: 'v'},
			crdt.InsertOp{ID: crdt.ID{Client: 3, Clock: 0}, Origin: crdt.StartID, RightOrigin: v, Content: 'p'},
			crdt.InsertOp{ID: crdt.ID{Client: 2, Clock: 0}, Origin: crdt.StartID, RightOrigin: crdt.EndID, Content: 'c'},
		}, "pvc", true
	}
	return nil, "", false
}

func runCase(name string) {
	ops, want, ok := counterexample(name)
	if !ok {
		fmt.Println("unknown case; try: case mlq  |  case pvc")
		return
	}
	labels := []string{"l", "q", "m"}
	if name == "pvc" {
		labels = []string{"v", "p", "c"}
	}
	allSame := true
	for _, order := range [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		d := crdt.NewDoc(9)
		names := make([]string, 3)
		for i, idx := range order {
			d.Receive(ops[idx])
			names[i] = labels[idx]
		}
		got := d.String()
		mark := "ok  "
		if got != want || d.PendingLen() != 0 {
			mark = "FAIL"
			allSame = false
		}
		fmt.Printf("  %s deliver %s -> %q  pending:%d\n", mark, strings.Join(names, " "), got, d.PendingLen())
	}
	if allSame {
		fmt.Printf("  all 6 orders agree on %q\n", want)
	} else {
		fmt.Printf("  DIVERGED: expected %q in every order\n", want)
	}
}

func check() {
	for _, n := range []string{"a", "b"} {
		if err := peers[n].doc.Check(); err != nil {
			fmt.Printf("  %s FAIL: %v\n", n, err)
		} else {
			fmt.Printf("  %s ok (%d items incl. tombstones)\n", n, len(peers[n].doc.Order()))
		}
	}
	if peers["a"].doc.String() != peers["b"].doc.String() {
		fmt.Println("  note: a and b differ (expected until you send/sync)")
	}
}

func showSV() {
	for _, n := range []string{"a", "b"} {
		fmt.Printf("  %s sv=%v pending=%d\n", n, peers[n].doc.StateVector(), peers[n].doc.PendingLen())
	}
}

func showDiff(from, to string) {
	src, dst := peers[from], peers[to]
	if src == nil || dst == nil {
		fmt.Println("usage: diff a b")
		return
	}
	ops := src.doc.Diff(dst.doc.StateVector())
	fmt.Printf("  %s would send %s %d ops:\n", from, to, len(ops))
	for _, op := range ops {
		switch o := op.(type) {
		case crdt.InsertOp:
			fmt.Printf("    insert %v %q\n", o.ID, o.Content)
		case crdt.DeleteOp:
			fmt.Printf("    delete %v target %v\n", o.ID, o.Target)
		}
	}
}

var lastSent crdt.Op
var lastSentTo string

func dup() {
	if lastSent == nil {
		fmt.Println("  nothing sent yet")
		return
	}
	p := peers[lastSentTo]
	applied := p.doc.Receive(lastSent)
	fmt.Printf("  re-sent %v to %s: applied %d (want 0), pending %d\n",
		lastSent.OpID(), p.name, len(applied), p.doc.PendingLen())
}

func fuzz(steps int) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	names := []string{"a", "b"}
	for i := 0; i < steps; i++ {
		p := peers[names[rng.Intn(2)]]
		n := len([]rune(p.doc.String()))
		if n == 0 || rng.Intn(4) != 0 {
			op, err := p.doc.LocalInsert(rng.Intn(n+1), rune('a'+rng.Intn(26)))
			if err == nil {
				p.queue = append(p.queue, op)
			}
		} else {
			op, err := p.doc.LocalDelete(rng.Intn(n))
			if err == nil {
				p.queue = append(p.queue, op)
			}
		}
		if rng.Intn(3) == 0 {
			other := peers["a"]
			if p == peers["a"] {
				other = peers["b"]
			}
			if len(p.queue) > 0 {
				k := rng.Intn(len(p.queue))
				other.doc.Receive(p.queue[k])
				p.queue = append(p.queue[:k], p.queue[k+1:]...)
			}
		}
	}
	sync()
	fmt.Printf("  %d random steps done\n", steps)
	check()
	a, b := peers["a"].doc, peers["b"].doc
	if a.String() == b.String() && slices.Equal(a.Order(), b.Order()) {
		fmt.Printf("  CONVERGED: both %d chars, identical item order\n", len([]rune(a.String())))
	} else {
		fmt.Println("  DIVERGED: a and b disagree")
	}
}

func status() {
	for _, n := range []string{"a", "b"} {
		p := peers[n]
		fmt.Printf("  %s = %-20q queued:%d pending:%d\n",
			p.name, p.doc.String(), len(p.queue), p.doc.PendingLen())
	}
}
