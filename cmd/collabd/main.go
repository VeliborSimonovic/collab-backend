package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"flag"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/veliborsimonovic/collab/demo"
	"github.com/veliborsimonovic/collab/server"
	"github.com/veliborsimonovic/collab/store"
)

type config struct {
	addr       string
	dbPath     string
	mem        bool
	publicKey  string
	origins    []string
	idle       time.Duration
	maxClients int
	maxItems   int
	dev        bool
	flush      time.Duration
	pprof      string

	ephemeral  bool
	demoKey    string
	demoTTL    time.Duration
	trustProxy bool
	demoPerIP  int
	demoWindow time.Duration
	maxText    int
	maxOps     int
	rate       float64
	burst      int
	maxMessage int
}

func envString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Fatalf("%s: invalid integer %q", key, v)
	}
	return n
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		log.Fatalf("%s: invalid boolean %q (use 1, true, 0 or false)", key, v)
	}
	return b
}

func envFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		log.Fatalf("%s: invalid number %q", key, v)
	}
	return f
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Fatalf("%s: invalid duration %q (use e.g. 5m or 30s)", key, v)
	}
	return d
}

func loadConfig() config {
	cfg := config{
		addr:       envString("COLLAB_ADDR", ":8080"),
		dbPath:     envString("COLLAB_DB", "collab.db"),
		publicKey:  os.Getenv("COLLAB_PUBLIC_KEY"),
		idle:       envDuration("COLLAB_IDLE", 5*time.Minute),
		maxClients: envInt("COLLAB_MAX_CLIENTS", 100),
		maxItems:   envInt("COLLAB_MAX_ITEMS", 1_000_000),
		dev:        envBool("COLLAB_DEV", false),
		flush:      envDuration("COLLAB_FLUSH", 10*time.Millisecond),
		pprof:      envString("COLLAB_PPROF", ""),

		ephemeral:  envBool("COLLAB_EPHEMERAL", false),
		demoKey:    os.Getenv("COLLAB_DEMO_KEY"),
		demoTTL:    envDuration("COLLAB_DEMO_TTL", 5*time.Minute),
		trustProxy: envBool("COLLAB_TRUST_PROXY", false),
		demoPerIP:  envInt("COLLAB_DEMO_ROOMS_PER_IP", 1),
		demoWindow: envDuration("COLLAB_DEMO_ROOM_WINDOW", time.Hour),
		maxText:    envInt("COLLAB_MAX_TEXT", 0),
		maxOps:     envInt("COLLAB_MAX_OPS", 0),
		rate:       envFloat("COLLAB_RATE", 0),
		burst:      envInt("COLLAB_BURST", 0),
		maxMessage: envInt("COLLAB_MAX_MESSAGE", 8<<20),
	}
	if raw := os.Getenv("COLLAB_ORIGINS"); raw != "" {
		for _, o := range strings.Split(raw, ",") {
			cfg.origins = append(cfg.origins, strings.TrimSpace(o))
		}
	}

	flag.StringVar(&cfg.addr, "addr", cfg.addr, "listen address (env COLLAB_ADDR)")
	flag.StringVar(&cfg.dbPath, "db", cfg.dbPath, "SQLite database path (env COLLAB_DB)")
	flag.BoolVar(&cfg.mem, "mem", false, "use the in-memory store instead of SQLite")
	flag.BoolVar(&cfg.dev, "dev", cfg.dev, "dev mode: no auth, everyone is an editor; local testing only (env COLLAB_DEV)")
	flag.DurationVar(&cfg.flush, "flush", cfg.flush, "interval between SQLite write batches (env COLLAB_FLUSH)")
	flag.StringVar(&cfg.pprof, "pprof", cfg.pprof, "address for the pprof profiling server, empty = off; never expose it publicly (env COLLAB_PPROF)")
	flag.BoolVar(&cfg.ephemeral, "ephemeral", cfg.ephemeral, "ephemeral mode: documents are dropped when the last client leaves; needs -mem (env COLLAB_EPHEMERAL)")
	flag.StringVar(&cfg.demoKey, "demo-key", cfg.demoKey, "PEM file with the demo private key, empty = demo off; needs -mem (env COLLAB_DEMO_KEY)")
	flag.DurationVar(&cfg.demoTTL, "demo-ttl", cfg.demoTTL, "how long a demo room lasts (env COLLAB_DEMO_TTL)")
	flag.BoolVar(&cfg.trustProxy, "trust-proxy", cfg.trustProxy, "take the client IP from X-Forwarded-For; only behind a reverse proxy (env COLLAB_TRUST_PROXY)")
	flag.Parse()

	if cfg.rate > 0 && cfg.burst == 0 {
		cfg.burst = max(cfg.maxText, 100)
	}

	return cfg
}

func makeAuth(publicKey string, dev bool) server.Authenticator {

	if publicKey != "" && dev {
		log.Print("-dev flag ignored, COLLAB_PUBLIC_KEY is set")
	}

	if publicKey == "" && dev {
		return server.NewDevAuth()
	}
	if publicKey == "" && !dev {
		log.Fatal("COLLAB_PUBLIC_KEY is not set: set it (see `collabd keygen`), or pass -dev / COLLAB_DEV=1 for local testing only")
	}

	key, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		log.Fatal("COLLAB_PUBLIC_KEY must be a base64-encoded 32-byte Ed25519 public key (see `collabd keygen`)")
	}
	return server.NewTokenAuth(key)
}

func main() {
	if len(os.Args) > 1 {
		if os.Args[1] == "keygen" {
			runKeygen()
			return
		}
		if os.Args[1] == "token" {
			runToken(os.Args[2:])
			return
		}
	}

	cfg := loadConfig()

	if cfg.ephemeral && !cfg.mem {
		log.Fatal("ephemeral mode needs -mem")
	}
	var demoPriv ed25519.PrivateKey
	if cfg.demoKey != "" {
		if !cfg.mem {
			log.Fatal("the demo needs -mem")
		}
		cfg.ephemeral = true

		var err error
		demoPriv, err = loadPrivateKey(cfg.demoKey)
		if err != nil {
			log.Fatalf("demo key: %v", err)
		}
		pub, err := base64.StdEncoding.DecodeString(cfg.publicKey)
		if err != nil || !demoPriv.Public().(ed25519.PublicKey).Equal(ed25519.PublicKey(pub)) {
			log.Fatal("demo key does not match COLLAB_PUBLIC_KEY")
		}
	}

	var st store.Store
	if cfg.mem {
		st = store.NewMemory()
	} else {
		sq, err := store.OpenSQLite(cfg.dbPath)
		if err != nil {
			log.Fatalf("open database %q: %v", cfg.dbPath, err)
		}
		st = store.NewBatched(sq, cfg.flush)
	}

	hub := server.NewHub(st, server.Limits{
		MaxClients: cfg.maxClients,
		MaxItems:   cfg.maxItems,
		MaxText:    cfg.maxText,
		MaxOps:     cfg.maxOps,
		Rate:       cfg.rate,
		Burst:      cfg.burst,
	}, cfg.idle)
	if cfg.ephemeral {
		hub.EnableEphemeral()
	}
	srv := server.NewServer(hub, cfg.origins, makeAuth(cfg.publicKey, cfg.dev))
	srv.SetMaxMessage(int64(cfg.maxMessage))

	mux := http.NewServeMux()
	mux.Handle("/", srv.Handler())
	if demoPriv != nil {
		demo.New(demo.Config{
			Key:          demoPriv,
			TTL:          cfg.demoTTL,
			PerIP:        cfg.demoPerIP,
			CreateWindow: cfg.demoWindow,
			MaxText:      cfg.maxText,
			TrustProxy:   cfg.trustProxy,
		}).Register(mux)
	}

	switch {
	case demoPriv != nil:
		log.Printf("demo mode, rooms last %s", cfg.demoTTL)
	case cfg.publicKey == "" && cfg.dev:
		log.Print("dev mode")
	default:
		log.Print("token mode")
	}

	httpSrv := &http.Server{
		Addr:    cfg.addr,
		Handler: mux,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("listening on %s", cfg.addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("ListenAndServe: %v", err)
		}
	}()

	if cfg.pprof != "" {
		go func() {
			log.Printf("pprof listening on %s", cfg.pprof)
			if err := http.ListenAndServe(cfg.pprof, nil); err != nil {
				log.Printf("pprof: %v", err)
			}
		}()
	}

	<-ctx.Done()
	log.Println("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Shutdown: %v", err)
	}
	if err := st.Close(); err != nil {
		log.Printf("store Close: %v", err)
	}
}
