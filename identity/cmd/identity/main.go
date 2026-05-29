package main

import (
	"database/sql"
	"log"
	"net/http"

	_ "github.com/lib/pq"

	"github.com/gazizov-ai/rsoi-project/identity/internal/config"
	"github.com/gazizov-ai/rsoi-project/identity/internal/httpserver"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to read env: %v", err)
	}

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to open postgres: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatalf("failed to ping postgres: %v", err)
	}

	srv, err := httpserver.New(db, cfg)
	if err != nil {
		log.Fatalf("failed to initialize identity server: %v", err)
	}

	log.Printf("identity listening on %s", cfg.Address())
	if err := http.ListenAndServe(cfg.Address(), srv.Router()); err != nil {
		log.Fatal(err)
	}
}
