package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"

	_ "github.com/lib/pq"

	"github.com/gazizov-ai/rsoi-project/common/auth"
	"github.com/gazizov-ai/rsoi-project/common/httputil"
	"github.com/gazizov-ai/rsoi-project/loyalty/internal/config"
)

type loyalty struct {
	ID               int64  `json:"-"`
	Username         string `json:"username"`
	ReservationCount int    `json:"reservationCount"`
	Status           string `json:"status"`
	Discount         int    `json:"discount"`
}

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
		log.Fatalf("failed to migrate loyalty db: %v", err)
	}

	validator := auth.NewValidator(cfg.IdentityURL+"/api/v1/jwks", cfg.JWTIssuer)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /manage/health", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("status: ok")) })
	mux.Handle("/api/v1/", validator.Middleware(s.routes()))

	log.Printf("loyalty listening on %s", cfg.Address())
	if err := http.ListenAndServe(cfg.Address(), withCORS(mux)); err != nil {
		log.Fatal(err)
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/loyalty", s.current)
	mux.HandleFunc("POST /api/v1/loyalty/increase", s.increase)
	mux.HandleFunc("POST /api/v1/loyalty/decrease", s.decrease)
	return mux
}

func (s *server) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS loyalty (
	id SERIAL PRIMARY KEY,
	username VARCHAR(80) NOT NULL UNIQUE,
	reservation_count INT NOT NULL DEFAULT 0,
	status VARCHAR(80) NOT NULL DEFAULT 'BRONZE' CHECK (status IN ('BRONZE', 'SILVER', 'GOLD')),
	discount INT NOT NULL DEFAULT 5,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DO $$
BEGIN
	IF to_regclass('public.loyalties') IS NOT NULL THEN
		INSERT INTO loyalty(username, reservation_count, status, discount)
		SELECT username, reservation_count, status, discount
		FROM loyalties
		ON CONFLICT (username) DO NOTHING;
	END IF;
END $$;

INSERT INTO loyalty(username, reservation_count, status, discount)
VALUES ('Test Max', 25, 'GOLD', 10)
ON CONFLICT (username) DO UPDATE
SET reservation_count = GREATEST(loyalty.reservation_count, EXCLUDED.reservation_count),
	status = CASE WHEN GREATEST(loyalty.reservation_count, EXCLUDED.reservation_count) >= 20 THEN 'GOLD' ELSE loyalty.status END,
	discount = CASE WHEN GREATEST(loyalty.reservation_count, EXCLUDED.reservation_count) >= 20 THEN 10 ELSE loyalty.discount END;
`)
	return err
}

func (s *server) current(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.FromContext(r.Context())
	record, err := s.ensure(r.Context(), claims.Username)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to get loyalty")
		return
	}
	httputil.JSON(w, http.StatusOK, record)
}

func (s *server) increase(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.FromContext(r.Context())
	record, err := s.change(r.Context(), claims.Username, 1)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to increase loyalty")
		return
	}
	httputil.JSON(w, http.StatusOK, record)
}

func (s *server) decrease(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.FromContext(r.Context())
	record, err := s.change(r.Context(), claims.Username, -1)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to decrease loyalty")
		return
	}
	httputil.JSON(w, http.StatusOK, record)
}

func (s *server) ensure(ctx context.Context, username string) (loyalty, error) {
	record, err := s.byUsername(ctx, username)
	if err == nil {
		return record, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return loyalty{}, err
	}
	var created loyalty
	err = s.db.QueryRowContext(ctx, `
INSERT INTO loyalty(username, reservation_count, status, discount)
VALUES ($1, 0, 'BRONZE', 5)
RETURNING id, username, reservation_count, status, discount
`, username).Scan(&created.ID, &created.Username, &created.ReservationCount, &created.Status, &created.Discount)
	return created, err
}

func (s *server) change(ctx context.Context, username string, delta int) (loyalty, error) {
	record, err := s.ensure(ctx, username)
	if err != nil {
		return loyalty{}, err
	}
	record.ReservationCount += delta
	if record.ReservationCount < 0 {
		record.ReservationCount = 0
	}
	record.Status, record.Discount = statusFor(record.ReservationCount)
	err = s.db.QueryRowContext(ctx, `
UPDATE loyalty
SET reservation_count = $2, status = $3, discount = $4
WHERE username = $1
RETURNING id, username, reservation_count, status, discount
`, username, record.ReservationCount, record.Status, record.Discount).
		Scan(&record.ID, &record.Username, &record.ReservationCount, &record.Status, &record.Discount)
	return record, err
}

func (s *server) byUsername(ctx context.Context, username string) (loyalty, error) {
	var record loyalty
	err := s.db.QueryRowContext(ctx, `
SELECT id, username, reservation_count, status, discount
FROM loyalty
WHERE username = $1
`, username).Scan(&record.ID, &record.Username, &record.ReservationCount, &record.Status, &record.Discount)
	return record, err
}

func statusFor(count int) (string, int) {
	switch {
	case count >= 20:
		return "GOLD", 10
	case count >= 10:
		return "SILVER", 7
	default:
		return "BRONZE", 5
	}
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
