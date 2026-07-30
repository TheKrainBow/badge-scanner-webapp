// Command server runs the badge-scanner webapp backend: REST API doing the
// CA/42 fetches, SQLite storage, user-account session auth for the
// dashboard, and an optional scoped API-key gate for external non-browser
// clients (see internal/auth's package doc comment).
package main

import (
	"log"
	"net/http"
	"os"

	"badgescanner/backend/internal/api"
	"badgescanner/backend/internal/auth"
	"badgescanner/backend/internal/caclient"
	"badgescanner/backend/internal/intraclient"
	"badgescanner/backend/internal/service"
	"badgescanner/backend/internal/store"
	"badgescanner/backend/internal/wshub"
)

func main() {
	addr := envOr("LISTEN_ADDR", ":8080")
	dbPath := envOr("DB_PATH", "badgescanner.db")

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET must be set (used to sign session cookies)")
	}

	secureCookie := envOr("COOKIE_SECURE", "true") != "false"

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	authSvc := auth.NewService(st, jwtSecret, secureCookie)
	if err := authSvc.Bootstrap(os.Getenv("ADMIN_USERNAME"), os.Getenv("ADMIN_PASSWORD")); err != nil {
		log.Fatalf("bootstrap admin: %v", err)
	}
	if err := authSvc.BootstrapAPIKey(os.Getenv("BOOTSTRAP_API_KEY_ID"), os.Getenv("BOOTSTRAP_API_KEY_SECRET")); err != nil {
		log.Fatalf("bootstrap API key: %v", err)
	}

	caClient, err := caclient.New()
	if err != nil {
		log.Fatalf("create CA client: %v", err)
	}
	intraClient := intraclient.New()

	hub := wshub.New()
	svc := service.New(st, caClient, intraClient)
	svc.SetEvents(hub)
	router := api.NewRouter(svc, authSvc, hub)

	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
