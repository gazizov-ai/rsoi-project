package main

import (
	"log"
	"net/http"

	"github.com/gazizov-ai/rsoi-project/gateway/config"
	transport "github.com/gazizov-ai/rsoi-project/gateway/transport/http"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to read env: %v", err)
	}

	r := transport.NewRouter(cfg)

	srv := &http.Server{
		Addr:    cfg.Address(),
		Handler: r,
	}

	log.Printf("gateway listening on %s", cfg.Address())
	log.Fatal(srv.ListenAndServe())
}
