// Command gem-server runs the Splendor HTTP + WebSocket server.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gem/internal/api"
	"gem/internal/auth"
	"gem/internal/game"
	"gem/internal/store"
	"gem/web"
)

func main() {
	addr := flag.String("addr", envOr("GEM_ADDR", ":8080"), "listen address")
	dbPath := flag.String("db", envOr("GEM_DB", "gem.db"), "SQLite database path")
	secret := flag.String("secret", os.Getenv("GEM_JWT_SECRET"), "JWT signing secret (random if empty: tokens expire on restart)")
	static := flag.String("static", os.Getenv("GEM_STATIC"), "serve the web client from this directory instead of the embedded build")
	hardMs := flag.Int("hard-ms", 1500, "hard AI thinking time per move (ms)")
	hintMs := flag.Int("hint-ms", 1500, "analysis time for hints (ms)")
	aiDelay := flag.Int("ai-delay-ms", 600, "minimum display delay of AI moves (ms)")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()
	if n, err := st.AbortUnfinished(context.Background()); err != nil {
		log.Fatalf("db: %v", err)
	} else if n > 0 {
		log.Printf("marked %d unfinished games from a previous run as aborted", n)
	}

	key := []byte(*secret)
	if len(key) == 0 {
		b := make([]byte, 32)
		rand.Read(b)
		key = []byte(hex.EncodeToString(b))
		log.Printf("GEM_JWT_SECRET not set; using a random secret")
	}
	cfg := game.DefaultConfig()
	cfg.HardBudget = time.Duration(*hardMs) * time.Millisecond
	cfg.HintBudget = time.Duration(*hintMs) * time.Millisecond
	cfg.AIDelay = time.Duration(*aiDelay) * time.Millisecond
	hub := game.NewHub(st, cfg)

	var files fs.FS = web.Dist()
	if *static != "" {
		files = os.DirFS(*static)
	}
	srv := &api.Server{Auth: auth.New(st, key), Store: st, Hub: hub, Static: files}
	hs := &http.Server{Addr: *addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
		<-ch
		log.Printf("shutting down")
		api.Shutdown(hs, hub)
	}()
	if files == nil {
		log.Printf("web client not embedded (build with -tags embed or pass -static web/dist)")
	}
	log.Printf("gem-server listening on %s", *addr)
	if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
