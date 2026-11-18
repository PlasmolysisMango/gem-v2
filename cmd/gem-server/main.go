// Command gem-server runs the Splendor HTTP + WebSocket server.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"gem/internal/api"
	"gem/internal/auth"
	"gem/internal/entryauth"
	"gem/internal/game"
	"gem/internal/replay"
	"gem/internal/store"
	"gem/web"
)

func main() {
	addr := flag.String("addr", envOr("GEM_ADDR", ":8080"), "listen address")
	dbPath := flag.String("db", envOr("GEM_DB", "gem.db"), "SQLite database path")
	secret := flag.String("secret", "", "JWT signing secret (random if empty: tokens expire on restart)")
	static := flag.String("static", os.Getenv("GEM_STATIC"), "serve the web client from this directory instead of the embedded build")
	adminUser := flag.String("admin-user", envOr("GEM_ADMIN_USER", "mango"), "promote this existing account to admin at startup (empty disables)")
	idleClose := flag.Duration("idle-close", envDuration("GEM_IDLE_CLOSE", 168*time.Hour), "close rooms with no clients after this long (0 disables)")
	hardMs := flag.Int("hard-ms", 300, "hard AI thinking time per move (ms)")
	masterMs := flag.Int("master-ms", 1500, "master AI thinking time per move (ms)")
	hintMs := flag.Int("hint-ms", 1500, "master AI analysis time for hints (ms)")
	aiDelay := flag.Int("ai-delay-ms", 600, "minimum display delay of AI moves (ms)")
	worker := flag.Bool("replay-worker", false, "run only the idle replay worker (CPU cap is provided by systemd)")
	replayCPU := flag.Int("replay-cpu-ms", 1500, "background replay search CPU budget per move (ms)")
	flag.Parse()
	if *worker {
		runtime.GOMAXPROCS(1)
		if *replayCPU <= 0 {
			log.Fatal("replay-cpu-ms must be > 0")
		}
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()
	if *worker {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := replay.RunWorker(ctx, st, time.Duration(*replayCPU)*time.Millisecond); err != nil {
			log.Printf("replay worker stopped: %v", err)
		}
		return
	}
	if *secret == "" {
		*secret = os.Getenv("GEM_JWT_SECRET")
	}
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
	cfg.MasterBudget = time.Duration(*masterMs) * time.Millisecond
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

	if err := st.BackfillReplayJobs(ctx); err != nil {
		log.Fatalf("replay queue: %v", err)
	}

	var files fs.FS = web.Dist()
	if *static != "" {
		files = os.DirFS(*static)
	}
	authSvc := auth.New(st, key)
	gateway, err := entryauth.New("gem")
	if err != nil {
		log.Fatalf("init entry gateway: %v", err)
	}
	authSvc.Entry = gateway
	if gateway != nil {
		log.Printf("entry gateway auth enabled")
	}
	srv := &api.Server{Auth: authSvc, Store: st, Hub: hub, Static: files}
	serverCtx, stopServer := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopServer()
	hs := &http.Server{Addr: *addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second,
		BaseContext: func(net.Listener) context.Context { return serverCtx }}
	shutdownDone := make(chan struct{})
	go func() {
		<-serverCtx.Done()
		log.Printf("shutting down")
		api.Shutdown(hs, hub)
		close(shutdownDone)
	}()
	if files == nil {
		log.Printf("web client not embedded (build with -tags embed or pass -static web/dist)")
	}
	log.Printf("gem-server listening on %s", *addr)
	if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	<-shutdownDone
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
