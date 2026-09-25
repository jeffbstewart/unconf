// Command unconf runs the unconference server: API, WebSocket, and the
// embedded frontend in one binary.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jeffbstewart/unconf/internal/server"
	"github.com/jeffbstewart/unconf/internal/store"
	"github.com/jeffbstewart/unconf/web"
)

func main() {
	addr := envOr("UNCONF_ADDR", ":8080")
	dbPath := envOr("UNCONF_DB", "unconf.db")
	eventName := envOr("UNCONF_EVENT_NAME", "Unconference")
	adminKey := os.Getenv("UNCONF_ADMIN_KEY")
	if adminKey == "" {
		log.Fatal("UNCONF_ADMIN_KEY is required (the key that grants organizer at login)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer st.Close()

	event, err := st.EnsureDefaultEvent(ctx, eventName)
	if err != nil {
		log.Fatalf("default event: %v", err)
	}

	secret, err := sessionSecret(ctx, st)
	if err != nil {
		log.Fatalf("session secret: %v", err)
	}

	app, err := server.New(server.Config{
		Store:         st,
		EventID:       event.ID,
		AdminKey:      adminKey,
		SessionSecret: secret,
		Static:        web.Dist(),
	})
	if err != nil {
		log.Fatalf("start server: %v", err)
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           app,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("unconf serving %q on %s (db %s)", event.Name, addr, dbPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	// Shutdown doesn't track hijacked WebSocket connections; closing the app
	// disconnects them and stops the hub before the database closes.
	app.Close()
}

// sessionSecret returns UNCONF_SESSION_SECRET, or a random secret generated
// on first run and persisted in meta so sessions survive restarts.
func sessionSecret(ctx context.Context, st *store.Store) ([]byte, error) {
	if v := os.Getenv("UNCONF_SESSION_SECRET"); v != "" {
		return []byte(v), nil
	}
	v, err := st.MetaOrInit(ctx, "session_secret", func() string {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			panic(err)
		}
		return hex.EncodeToString(b)
	})
	return []byte(v), err
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
