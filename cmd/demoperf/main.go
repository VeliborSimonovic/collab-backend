package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/veliborsimonovic/collab/crdt"
)

type cl struct {
	name      string
	mu        sync.Mutex
	doc       *crdt.Doc
	conn      *websocket.Conn
	sent      map[crdt.ID]time.Time
	lat       chan time.Duration
	seen      *sync.Map
	got       chan struct{}
	closeCode websocket.StatusCode
	closed    chan struct{}
}

func post(base, path string, body any) (map[string]any, int, error) {
	b, _ := json.Marshal(body)
	res, err := http.Post(base+path, "application/json", bytes.NewReader(b))
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m == nil {
		m = map[string]any{"raw": string(raw)}
	}
	return m, res.StatusCode, nil
}

func pct(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	s := slices.Clone(d)
	slices.Sort(s)
	i := int(float64(len(s)-1) * p)
	return s[i]
}

func report(label string, d []time.Duration) {
	if len(d) == 0 {
		fmt.Printf("%-34s no samples\n", label)
		return
	}
	var sum time.Duration
	for _, v := range d {
		sum += v
	}
	fmt.Printf("%-34s n=%-5d avg=%-8v p50=%-8v p95=%-8v p99=%-8v max=%v\n", label, len(d),
		(sum / time.Duration(len(d))).Round(10*time.Microsecond), pct(d, .5).Round(10*time.Microsecond),
		pct(d, .95).Round(10*time.Microsecond), pct(d, .99).Round(10*time.Microsecond), pct(d, 1).Round(10*time.Microsecond))
}

func main() {
	base := flag.String("base", "https://collab-demo.veliborsimonovic.dev", "")
	people := flag.Int("people", 4, "")
	joinCode := flag.String("code", "", "join this existing room instead of creating one")
	flag.Parse()
	ctx := context.Background()

	var urls []string
	code := *joinCode
	if code == "" {
		t0 := time.Now()
		m, st, err := post(*base, "/demo/rooms", map[string]string{"name": "perf0"})
		if err != nil || st != 200 {
			fmt.Println("create room failed:", st, m, err)
			return
		}
		fmt.Printf("create room: %v (HTTP %d)\n", time.Since(t0).Round(time.Millisecond), st)
		urls = append(urls, m["url"].(string))
		if u, _ := url.Parse(urls[0]); u != nil {
			code = u.Query().Get("code")
		}
	}
	for i := len(urls); i < *people; i++ {
		t := time.Now()
		jm, jst, err := post(*base, "/demo/rooms/join", map[string]string{"code": code, "name": fmt.Sprintf("perf%d", i)})
		if err != nil || jst != 200 {
			fmt.Println("join failed:", jst, jm, err)
			return
		}
		fmt.Printf("join %d: %v\n", i, time.Since(t).Round(time.Millisecond))
		urls = append(urls, jm["url"].(string))
	}

	wsBase := strings.Replace(*base, "http", "ws", 1)
	shared := &sync.Map{}
	var clients []*cl
	lats := make([][]time.Duration, *people)
	var latMu sync.Mutex
	var phase string
	phaseLats := map[string][]time.Duration{}

	for i, u := range urls {
		pu, _ := url.Parse(u)
		q := pu.Query()
		c := &cl{name: fmt.Sprintf("perf%d", i), doc: crdt.NewDoc(crdt.ClientID(rand.Uint64N(1<<48) + 1<<20)), seen: shared, closed: make(chan struct{})}
		t := time.Now()
		conn, _, err := websocket.Dial(ctx, wsBase+"/ws?doc="+url.QueryEscape(q.Get("doc"))+"&token="+url.QueryEscape(q.Get("token")), nil)
		if err != nil {
			fmt.Println("ws dial:", err)
			return
		}
		conn.SetReadLimit(8 << 20)
		c.conn = conn
		sv := crdt.EncodeSV(c.doc.StateVector())
		_ = conn.Write(ctx, websocket.MessageBinary, append([]byte{1}, sv...))
		fmt.Printf("ws connect %d: %v\n", i, time.Since(t).Round(time.Millisecond))
		idx := i
		go func() {
			for {
				_, msg, err := conn.Read(ctx)
				if err != nil {
					c.closeCode = websocket.CloseStatus(err)
					close(c.closed)
					return
				}
				now := time.Now()
				if len(msg) == 0 {
					continue
				}
				switch msg[0] {
				case 1:
					sv, _ := crdt.DecodeSV(msg[1:])
					c.mu.Lock()
					d := c.doc.Diff(sv)
					c.mu.Unlock()
					if len(d) > 0 {
						_ = conn.Write(ctx, websocket.MessageBinary, append([]byte{2}, crdt.EncodeOps(d)...))
					}
				case 2:
					ops, err := crdt.DecodeOps(msg[1:])
					if err != nil {
						continue
					}
					c.mu.Lock()
					c.doc.Receive(ops...)
					c.mu.Unlock()
					for _, op := range ops {
						var id crdt.ID
						switch o := op.(type) {
						case crdt.InsertOp:
							id = o.ID
						default:
							continue
						}
						if v, ok := shared.Load(id); ok {
							latMu.Lock()
							d := now.Sub(v.(time.Time))
							lats[idx] = append(lats[idx], d)
							phaseLats[phase] = append(phaseLats[phase], d)
							latMu.Unlock()
						}
					}
				}
			}
		}()
		clients = append(clients, c)
	}
	time.Sleep(1500 * time.Millisecond)

	typeOne := func(c *cl) error {
		c.mu.Lock()
		op, err := c.doc.LocalInsert(0, rune('a'+rand.IntN(26)))
		shared.Store(op.ID, time.Now())
		del, derr := c.doc.LocalDelete(0)
		c.mu.Unlock()
		if err != nil || derr != nil {
			return fmt.Errorf("local op: %v %v", err, derr)
		}
		return c.conn.Write(ctx, websocket.MessageBinary, append([]byte{2}, crdt.EncodeOps([]crdt.Op{op, del})...))
	}

	phase = "1 single typist 10 keys/s"
	for i := 0; i < 150; i++ {
		if err := typeOne(clients[0]); err != nil {
			fmt.Println("send:", err)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(time.Second)

	phase = fmt.Sprintf("2 all %d typing 5 keys/s each", *people)
	var wg sync.WaitGroup
	for _, c := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tk := time.NewTicker(200 * time.Millisecond)
			defer tk.Stop()
			end := time.After(20 * time.Second)
			for {
				select {
				case <-end:
					return
				case <-c.closed:
					return
				case <-tk.C:
					if typeOne(c) != nil {
						return
					}
				}
			}
		}()
	}
	wg.Wait()
	time.Sleep(time.Second)

	phase = "3 paste burst 500 chars"
	c := clients[0]
	c.mu.Lock()
	var ops []crdt.Op
	var lastID crdt.ID
	for i := 0; i < 500; i++ {
		op, err := c.doc.LocalInsert(i, 'x')
		if err != nil {
			break
		}
		ops = append(ops, op)
		lastID = op.ID
	}
	shared.Store(lastID, time.Now())
	c.mu.Unlock()
	_ = c.conn.Write(ctx, websocket.MessageBinary, append([]byte{2}, crdt.EncodeOps(ops)...))
	select {
	case <-c.closed:
		fmt.Printf("PASTE: sender was disconnected, close code %d\n", c.closeCode)
	case <-time.After(3 * time.Second):
		fmt.Println("PASTE: sender still connected after 3 s")
	}

	fmt.Println()
	latMu.Lock()
	for _, k := range []string{"1 single typist 10 keys/s", fmt.Sprintf("2 all %d typing 5 keys/s each", *people), "3 paste burst 500 chars"} {
		report("propagation "+k, phaseLats[k])
	}
	latMu.Unlock()

	time.Sleep(time.Second)
	texts := map[string]int{}
	for i, c := range clients {
		select {
		case <-c.closed:
			fmt.Printf("client %d: closed (code %d)\n", i, c.closeCode)
			continue
		default:
		}
		c.mu.Lock()
		texts[c.doc.String()]++
		c.mu.Unlock()
	}
	fmt.Printf("distinct document states among live clients: %d\n", len(texts))
}
