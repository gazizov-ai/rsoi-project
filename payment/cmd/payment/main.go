package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/segmentio/kafka-go"

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
	if strings.TrimSpace(cfg.KafkaBrokers) != "" {
		go s.consumePaymentCancel(context.Background(), cfg)
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

CREATE TABLE IF NOT EXISTS processed_events (
	event_id UUID PRIMARY KEY,
	processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
`)
	return err
}

type paymentCancelEvent struct {
	EventID        string    `json:"eventId"`
	ReservationUID string    `json:"reservationUid"`
	PaymentUID     string    `json:"paymentUid"`
	Username       string    `json:"username"`
	OccurredAt     time.Time `json:"occurredAt"`
}

func (s *server) consumePaymentCancel(ctx context.Context, cfg config.Config) {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  splitCSV(cfg.KafkaBrokers),
		Topic:    cfg.KafkaPaymentCancelTopic,
		GroupID:  cfg.KafkaGroupID,
		MinBytes: 1,
		MaxBytes: 10e6,
	})
	defer reader.Close()

	for {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			log.Printf("payment kafka read failed: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		var event paymentCancelEvent
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			log.Printf("invalid payment cancel event: %v", err)
			_ = reader.CommitMessages(ctx, msg)
			continue
		}
		if event.EventID == "" || event.PaymentUID == "" {
			log.Printf("invalid payment cancel event: missing eventId or paymentUid")
			_ = reader.CommitMessages(ctx, msg)
			continue
		}
		if _, err := uuid.Parse(event.EventID); err != nil {
			log.Printf("invalid payment cancel event: bad eventId %q", event.EventID)
			_ = reader.CommitMessages(ctx, msg)
			continue
		}
		if _, err := uuid.Parse(event.PaymentUID); err != nil {
			log.Printf("invalid payment cancel event: bad paymentUid %q", event.PaymentUID)
			_ = reader.CommitMessages(ctx, msg)
			continue
		}
		if err := s.applyPaymentCancel(ctx, event); err != nil {
			log.Printf("failed to apply payment cancel event %s: %v", event.EventID, err)
			continue
		}
		if err := reader.CommitMessages(ctx, msg); err != nil {
			log.Printf("failed to commit payment cancel event %s: %v", event.EventID, err)
		}
	}
}

func (s *server) applyPaymentCancel(ctx context.Context, event paymentCancelEvent) error {
	eventID, err := uuid.Parse(event.EventID)
	if err != nil {
		return err
	}
	paymentID, err := uuid.Parse(event.PaymentUID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var alreadyProcessed bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM processed_events WHERE event_id = $1)`, eventID).Scan(&alreadyProcessed); err != nil {
		return err
	}
	if alreadyProcessed {
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment SET status = 'CANCELED' WHERE payment_uid = $1`, paymentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO processed_events(event_id) VALUES ($1)`, eventID); err != nil {
		return err
	}
	return tx.Commit()
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
