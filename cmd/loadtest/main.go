package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/veliborsimonovic/collab/crdt"
	"github.com/veliborsimonovic/collab/server"

	"github.com/coder/websocket"
)

type client struct {
	mu   sync.Mutex
	doc  *crdt.Doc
	conn *websocket.Conn

	first     chan struct{}
	firstOnce *sync.Once
}

func newClient() *client {
	return &client{doc: crdt.NewDoc(crdt.ClientID(rand.Uint64N(1<<48) + 1<<20))}
}

func (c *client) connect(ctx context.Context, url, doc, name string) error {
	conn, _, err := websocket.Dial(ctx, url+"/ws?doc="+doc+"&name="+name, nil)
	if err != nil {
		return err
	}
	conn.SetReadLimit(8 << 20)
	c.conn = conn
	c.first = make(chan struct{})
	c.firstOnce = new(sync.Once)

	c.mu.Lock()
	sv := crdt.EncodeSV(c.doc.StateVector())
	c.mu.Unlock()
	return c.write(ctx, server.MsgSyncStep1, sv)
}

func (c *client) write(ctx context.Context, t byte, payload []byte) error {
	msg := make([]byte, 1+len(payload))
	msg[0] = t
	copy(msg[1:], payload)
	return c.conn.Write(ctx, websocket.MessageBinary, msg)
}

func (c *client) readLoop(ctx context.Context, onOps func([]crdt.Op)) error {
	for {
		_, msg, err := c.conn.Read(ctx)
		if err != nil {
			return err
		}
		if len(msg) == 0 {
			continue
		}
		first, once := c.first, c.firstOnce
		switch msg[0] {
		case server.MsgSyncStep1:
			sv, err := crdt.DecodeSV(msg[1:])
			if err != nil {
				return err
			}
			c.mu.Lock()
			diff := c.doc.Diff(sv)
			c.mu.Unlock()
			if len(diff) > 0 {
				if err := c.write(ctx, server.MsgUpdate, crdt.EncodeOps(diff)); err != nil {
					return err
				}
			}
		case server.MsgUpdate:
			ops, err := crdt.DecodeOps(msg[1:])
			if err != nil {
				return err
			}

			c.mu.Lock()
			c.doc.Receive(ops...)
			c.mu.Unlock()
			if onOps != nil {
				onOps(ops)
			}
		case server.MsgPresence, server.MsgPresenceGone:
		}
		once.Do(func() { close(first) })
	}
}

func (c *client) typeOne(ctx context.Context, send bool) error {
	c.mu.Lock()
	n := utf8.RuneCountInString(c.doc.String())
	op, err := c.doc.LocalInsert(rand.IntN(n+1), rune('a'+rand.IntN(26)))
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if !send {
		return nil
	}
	return c.write(ctx, server.MsgUpdate, crdt.EncodeOps([]crdt.Op{op}))
}

type registry struct {
	mu   sync.Mutex
	docs map[string][]*client
	errs []error

	kicks            atomic.Int64
	failedReconnects atomic.Int64
}

func (r *registry) add(doc string, c *client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.docs[doc] = append(r.docs[doc], c)
}

func (r *registry) fail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, err)
}

func (r *registry) watch(connCtx context.Context, c *client, onOps func([]crdt.Op)) {
	go func() {
		if err := c.readLoop(connCtx, onOps); err != nil && connCtx.Err() == nil {
			r.fail(err)
		}
	}()
}

// runClient types like a browser tab: when the connection drops while editing is
// still running, it counts a kick, waits a second and reconnects with the same
// client, so its copy catches up and the convergence check still includes it.
func runClient(editCtx, connCtx context.Context, url, doc string, rate time.Duration, reg *registry) {
	c := newClient()
	if err := c.connect(connCtx, url, doc, "load"); err != nil {
		reg.fail(err)
		return
	}
	reg.add(doc, c)

	t := time.NewTicker(rate)
	defer t.Stop()
	for {
		readDone := make(chan error, 1)
		go func() { readDone <- c.readLoop(connCtx, nil) }()

		// Like a browser, don't type until the handshake has started: an op sent
		// before the server has the ones it depends on is refused as a bad message.
		ready := c.first

	typing:
		for {
			select {
			case <-editCtx.Done():
				// Keep reading until connCtx ends so the last edits still arrive.
				go func() {
					if err := <-readDone; err != nil && connCtx.Err() == nil {
						reg.fail(err)
					}
				}()
				return
			case <-readDone:
				break typing
			case <-ready:
				ready = nil
			case <-t.C:
				if ready != nil {
					continue
				}
				if err := c.typeOne(connCtx, true); err != nil {
					// The write failed, so the connection is gone. Closing it ends readLoop.
					c.conn.CloseNow()
					<-readDone
					break typing
				}
			}
		}

		if editCtx.Err() != nil || connCtx.Err() != nil {
			return
		}
		reg.kicks.Add(1)
		select {
		case <-editCtx.Done():
			return
		case <-connCtx.Done():
			return
		case <-time.After(time.Second):
		}
		if err := c.connect(connCtx, url, doc, "load"); err != nil {
			reg.failedReconnects.Add(1)
			reg.fail(err)
			return
		}
	}
}

func runReconnector(editCtx context.Context, url, doc string, reg *registry) {
	c := newClient()
	for editCtx.Err() == nil {
		if !reconnectRound(editCtx, c, url, doc, reg) {
			return
		}
	}
}

func reconnectRound(editCtx context.Context, c *client, url, doc string, reg *registry) bool {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := c.connect(ctx, url, doc, "reconnector"); err != nil {
		reg.fail(err)
		return false
	}

	var closing atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := c.readLoop(ctx, nil); err != nil && !closing.Load() {
			reg.fail(err)
		}
	}()

	select {
	case <-c.first:
	case <-done:
	case <-editCtx.Done():
	}

	ok := true
	t := time.NewTicker(100 * time.Millisecond)
	deadline := time.After(3 * time.Second)
typing:
	for {
		select {
		case <-editCtx.Done():
			break typing
		case <-deadline:
			break typing
		case <-t.C:
			if err := c.typeOne(ctx, true); err != nil {
				reg.fail(err)
				ok = false
				break typing
			}
		}
	}
	t.Stop()

	closing.Store(true)
	c.conn.Close(websocket.StatusNormalClosure, "")
	cancel()
	<-done

	if !ok {
		return false
	}
	for range 5 {
		if err := c.typeOne(ctx, false); err != nil {
			reg.fail(err)
			return false
		}
	}
	return true
}

type latencyLog struct {
	mu      sync.Mutex
	samples []time.Duration
}

func runProbe(editCtx, connCtx context.Context, url, doc string, lat *latencyLog, reg *registry) {
	writer, reader := newClient(), newClient()
	for _, c := range []*client{writer, reader} {
		if err := c.connect(connCtx, url, doc, "probe"); err != nil {
			reg.fail(err)
			return
		}
		reg.add(doc, c)
	}

	var sentMu sync.Mutex
	sent := make(map[crdt.ID]time.Time)

	reg.watch(connCtx, writer, nil)
	reg.watch(connCtx, reader, func(ops []crdt.Op) {
		now := time.Now()
		sentMu.Lock()
		defer sentMu.Unlock()
		for _, op := range ops {
			if at, ok := sent[op.OpID()]; ok {
				lat.mu.Lock()
				lat.samples = append(lat.samples, now.Sub(at))
				lat.mu.Unlock()
				delete(sent, op.OpID())
			}
		}
	})

	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-editCtx.Done():
			return
		case <-t.C:
			writer.mu.Lock()
			n := utf8.RuneCountInString(writer.doc.String())
			op, err := writer.doc.LocalInsert(rand.IntN(n+1), rune('a'+rand.IntN(26)))
			writer.mu.Unlock()
			if err != nil {
				reg.fail(err)
				return
			}
			sentMu.Lock()
			sent[op.ID] = time.Now()
			sentMu.Unlock()
			if err := writer.write(connCtx, server.MsgUpdate, crdt.EncodeOps([]crdt.Op{op})); err != nil {
				reg.fail(err)
				return
			}
		}
	}
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[int(p*float64(len(sorted)-1))]
}

func text(c *client) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.doc.String()
}

func checkConverged(docs map[string][]*client) error {
	for id, cs := range docs {
		if len(cs) == 0 {
			continue
		}
		want := text(cs[0])
		for _, c := range cs[1:] {
			if got := text(c); got != want {
				return fmt.Errorf("doc %s: text lengths differ (%d vs %d)", id, len(want), len(got))
			}
		}
	}
	return nil
}

func main() {
	conns := flag.Int("conns", 200, "number of typing clients")
	docs := flag.Int("docs", 20, "number of documents")
	duration := flag.Duration("duration", 60*time.Second, "how long clients type")
	url := flag.String("url", "ws://localhost:8080", "server base URL")
	rate := flag.Duration("rate", 100*time.Millisecond, "time between keystrokes of each typing client")
	flag.Parse()

	editCtx, stopEdit := context.WithTimeout(context.Background(), *duration)
	defer stopEdit()
	connCtx, closeConns := context.WithCancel(context.Background())
	defer closeConns()

	reg := &registry{docs: make(map[string][]*client)}
	lat := &latencyLog{}
	var wg sync.WaitGroup

	for i := range *conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runClient(editCtx, connCtx, *url, fmt.Sprintf("load-%d", i%*docs), *rate, reg)
		}()
		time.Sleep(5 * time.Millisecond)
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		runReconnector(editCtx, *url, "load-0", reg)
	}()
	go func() {
		defer wg.Done()
		runProbe(editCtx, connCtx, *url, "load-1", lat, reg)
	}()

	wg.Wait()
	var convErr error
	for deadline := time.Now().Add(30 * time.Second); ; {
		reg.mu.Lock()
		convErr = checkConverged(reg.docs)
		reg.mu.Unlock()
		if convErr == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}

	reg.mu.Lock()
	errs := slices.Clone(reg.errs)
	nClients := 0
	for _, cs := range reg.docs {
		nClients += len(cs)
	}
	reg.mu.Unlock()

	closeConns()

	lat.mu.Lock()
	samples := slices.Clone(lat.samples)
	lat.mu.Unlock()
	slices.Sort(samples)

	fmt.Printf("connections: %d, docs: %d, duration: %s\n", *conns, *docs, *duration)
	fmt.Printf("latency samples: %d, p50: %s, p95: %s\n", len(samples), percentile(samples, 0.50), percentile(samples, 0.95))
	fmt.Printf("kicks: %d\n", reg.kicks.Load())
	fmt.Printf("failed reconnects: %d\n", reg.failedReconnects.Load())
	fmt.Printf("errors: %d\n", len(errs))
	counts := make(map[string]int)
	for _, err := range errs {
		counts[strings.ReplaceAll(err.Error(), "\n", " ")]++
	}
	for msg, n := range counts {
		fmt.Printf("  %dx %s\n", n, msg)
	}
	if nClients == 0 {
		fmt.Println("FAILED: no client connected; is the server running at", *url, "?")
		os.Exit(1)
	}
	if convErr != nil {
		fmt.Println("NOT CONVERGED:", convErr)
		os.Exit(1)
	}
	fmt.Println("all documents converged")
}
