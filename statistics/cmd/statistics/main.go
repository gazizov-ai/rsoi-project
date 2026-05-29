package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/segmentio/kafka-go"

	"github.com/gazizov-ai/rsoi-project/common/auth"
	"github.com/gazizov-ai/rsoi-project/common/httputil"
	"github.com/gazizov-ai/rsoi-project/statistics/internal/config"
)

type server struct {
	db *sql.DB
}

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
	s := &server{db: db}
	if err := s.migrate(context.Background()); err != nil {
		log.Fatalf("failed to migrate statistics db: %v", err)
	}
	if strings.TrimSpace(cfg.KafkaBrokers) != "" {
		go s.consumeKafka(context.Background(), cfg)
	}

	validator := auth.NewValidator(cfg.IdentityURL+"/api/v1/jwks", cfg.JWTIssuer)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /manage/health", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("status: ok")) })
	mux.Handle("POST /api/v1/events", validator.Middleware(http.HandlerFunc(s.ingest)))
	mux.Handle("GET /api/v1/statistics", validator.RequireRole("Admin", http.HandlerFunc(s.report)))

	log.Printf("statistics listening on %s", cfg.Address())
	if err := http.ListenAndServe(cfg.Address(), withCORS(mux)); err != nil {
		log.Fatal(err)
	}
}

func (s *server) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS events (
	id SERIAL PRIMARY KEY,
	type VARCHAR(120) NOT NULL,
	username VARCHAR(80) NOT NULL,
	payload JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS events_type_idx ON events(type);
CREATE INDEX IF NOT EXISTS events_username_idx ON events(username);
`)
	return err
}

func (s *server) ingest(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.FromContext(r.Context())
	var req struct {
		Type     string         `json:"type"`
		Username string         `json:"username"`
		Payload  map[string]any `json:"payload"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Type == "" {
		httputil.Error(w, http.StatusBadRequest, "event type is required")
		return
	}
	if req.Username == "" {
		req.Username = claims.Username
	}
	if req.Payload == nil {
		req.Payload = map[string]any{}
	}
	if err := s.storeEvent(r.Context(), req.Type, req.Username, req.Payload); err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to store event")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *server) consumeKafka(ctx context.Context, cfg config.Config) {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  splitCSV(cfg.KafkaBrokers),
		Topic:    cfg.KafkaTopic,
		GroupID:  cfg.KafkaGroupID,
		MinBytes: 1,
		MaxBytes: 10e6,
	})
	defer reader.Close()

	for {
		msg, err := reader.ReadMessage(ctx)
		if err != nil {
			log.Printf("kafka read failed: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		var event struct {
			Type     string         `json:"type"`
			Username string         `json:"username"`
			Payload  map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			log.Printf("invalid kafka event: %v", err)
			continue
		}
		if event.Type == "" || event.Username == "" {
			continue
		}
		if event.Payload == nil {
			event.Payload = map[string]any{}
		}
		if err := s.storeEvent(ctx, event.Type, event.Username, event.Payload); err != nil {
			log.Printf("failed to store kafka event: %v", err)
		}
	}
}

func (s *server) storeEvent(ctx context.Context, eventType, username string, payload map[string]any) error {
	data, _ := json.Marshal(payload)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO events(type, username, payload)
VALUES ($1, $2, $3)
`, eventType, username, string(data))
	return err
}

func (s *server) report(w http.ResponseWriter, r *http.Request) {
	eventsByType, err := s.groupCount(r.Context(), "type")
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to build report")
		return
	}
	eventsByUser, err := s.groupCount(r.Context(), "username")
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to build report")
		return
	}
	var total int64
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM events`).Scan(&total)
	httputil.JSON(w, http.StatusOK, map[string]any{
		"totalEvents":  total,
		"eventsByType": eventsByType,
		"eventsByUser": eventsByUser,
	})
}

func (s *server) groupCount(ctx context.Context, column string) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+column+`, COUNT(*) FROM events GROUP BY `+column+` ORDER BY COUNT(*) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]int64{}
	for rows.Next() {
		var key string
		var count int64
		if err := rows.Scan(&key, &count); err != nil {
			return nil, err
		}
		result[key] = count
	}
	return result, rows.Err()
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if strings.EqualFold(r.Method, http.MethodOptions) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
