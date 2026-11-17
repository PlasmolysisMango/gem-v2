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
	"strconv"
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
	adminUser := flag.String("admin-user", envOr("GEM_ADMIN_USER", "mango"), "promote this existing account to admin at startup (empty disables)")
	idleClose := flag.Duration("idle-close", envDuration("GEM_IDLE_CLOSE", 168*time.Hour), "close rooms with no clients after this long (0 disables)")
	hardMs := flag.Int("hard-ms", 1500, "hard AI thinking time per move (ms)")
	hintMs := flag.Int("hint-ms", 1500, "analysis time for hints (ms)")
	aiDelay := flag.Int("ai-delay-ms", 600, "minimum display delay of AI moves (ms)")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	if *idleClose < 0 {
		log.Fatalf("idle-close must be >= 0")
	}
	if *adminUser != "" {
		ok, err := st.PromoteAdmin(ctx, *adminUser)
		if err != nil {
			log.Fatalf("promote admin: %v", err)
		}
		if ok {
			log.Printf("administrator: %s", *adminUser)
		} else {
			log.Printf("admin user %q not found; register that account to use the admin page", *adminUser)
		}
	}
	idle := *idleClose
	if v, ok, err := st.GetSetting(ctx, store.SettingIdleClose); err != nil {
		log.Fatalf("db: %v", err)
	} else if !ok {
		if idle > 0 && idle < time.Second {
			idle = time.Second
		}
		if err := st.SetSetting(ctx, store.SettingIdleClose, strconv.FormatInt(int64(idle/time.Second), 10)); err != nil {
			log.Fatalf("db: %v", err)
		}
	} else if n, err := strconv.ParseInt(v, 10, 64); err != nil || n < 0 {
		log.Printf("invalid %s=%q; using %s", store.SettingIdleClose, v, idle)
	} else {
		idle = time.Duration(n) * time.Second
	}
	log.Printf("idle room cleanup: %s (0 = disabled)", idle)

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
	cfg.IdleClose = idle
	hub := game.NewHub(st, cfg)
	if err := hub.Restore(ctx); err != nil {
		log.Fatalf("restore rooms: %v", err)
	}
	if n, err := st.AbortOrphanGames(ctx); err != nil {
		log.Fatalf("db: %v", err)
	} else if n > 0 {
		log.Printf("marked %d orphaned games as aborted", n)
	}

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

func envDuration(k string, def time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Fatalf("invalid %s=%q: %v", k, v, err)
	}
	return d
}
