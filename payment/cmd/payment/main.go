package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"github.com/gazizov-ai/rsoi-project/common/auth"
	"github.com/gazizov-ai/rsoi-project/common/httputil"
	"github.com/gazizov-ai/rsoi-project/payment/internal/config"
)

type payment struct {
	ID        int64     `json:"-"`
	PaymentID uuid.UUID `json:"paymentUid"`
	Status    string    `json:"status"`
	Price     int       `json:"price"`
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
		log.Fatalf("failed to migrate payment db: %v", err)
	}

	validator := auth.NewValidator(cfg.IdentityURL+"/api/v1/jwks", cfg.JWTIssuer)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /manage/health", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("status: ok")) })
	mux.Handle("/api/v1/", validator.Middleware(s.routes()))

	log.Printf("payment listening on %s", cfg.Address())
	if err := http.ListenAndServe(cfg.Address(), withCORS(mux)); err != nil {
		log.Fatal(err)
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/payments", s.create)
	mux.HandleFunc("GET /api/v1/payments/{uid}", s.get)
	mux.HandleFunc("DELETE /api/v1/payments/{uid}", s.cancel)
	mux.HandleFunc("POST /api/v1/payments/{uid}/cancel", s.cancel)
	return mux
}

func (s *server) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS payment (
	id SERIAL PRIMARY KEY,
	payment_uid uuid NOT NULL UNIQUE,
	status VARCHAR(20) NOT NULL CHECK (status IN ('PAID', 'CANCELED')),
	price INT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
`)
	return err
}

func (s *server) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Price int `json:"price"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil || req.Price <= 0 {
		httputil.Error(w, http.StatusBadRequest, "invalid payment")
		return
	}
	p := payment{PaymentID: uuid.New(), Status: "PAID", Price: req.Price}
	err := s.db.QueryRowContext(r.Context(), `
INSERT INTO payment(payment_uid, status, price)
VALUES ($1, $2, $3)
RETURNING id
`, p.PaymentID, p.Status, p.Price).Scan(&p.ID)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to create payment")
		return
	}
	httputil.JSON(w, http.StatusCreated, p)
}

func (s *server) get(w http.ResponseWriter, r *http.Request) {
	p, err := s.paymentByUID(r.Context(), r.PathValue("uid"))
	if errors.Is(err, sql.ErrNoRows) {
		httputil.Error(w, http.StatusNotFound, "payment not found")
		return
	}
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to get payment")
		return
	}
	httputil.JSON(w, http.StatusOK, p)
}

func (s *server) cancel(w http.ResponseWriter, r *http.Request) {
	uid, err := uuid.Parse(r.PathValue("uid"))
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid payment uid")
		return
	}
	res, err := s.db.ExecContext(r.Context(), `
UPDATE payment SET status = 'CANCELED' WHERE payment_uid = $1
`, uid)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to cancel payment")
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		httputil.Error(w, http.StatusNotFound, "payment not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) paymentByUID(ctx context.Context, raw string) (payment, error) {
	uid, err := uuid.Parse(raw)
	if err != nil {
		return payment{}, sql.ErrNoRows
	}
	var p payment
	err = s.db.QueryRowContext(ctx, `
SELECT id, payment_uid, status, price
FROM payment
WHERE payment_uid = $1
`, uid).Scan(&p.ID, &p.PaymentID, &p.Status, &p.Price)
	return p, err
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		if strings.EqualFold(r.Method, http.MethodOptions) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), "startedAt", time.Now())))
	})
}
