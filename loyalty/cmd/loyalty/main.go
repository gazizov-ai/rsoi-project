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
	if strings.TrimSpace(cfg.KafkaBrokers) != "" {
		go s.consumeReservationCreated(context.Background(), cfg)
		go s.consumeReservationCanceled(context.Background(), cfg)
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

CREATE TABLE IF NOT EXISTS processed_events (
	event_id UUID PRIMARY KEY,
	processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS loyalty_reservations (
	reservation_uid UUID PRIMARY KEY,
	username VARCHAR(80) NOT NULL,
	active BOOLEAN NOT NULL DEFAULT true,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
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

type reservationCreatedEvent struct {
	EventID        string    `json:"eventId"`
	ReservationUID string    `json:"reservationUid"`
	PaymentUID     string    `json:"paymentUid"`
	Username       string    `json:"username"`
	Price          int       `json:"price"`
	Discount       int       `json:"discount"`
	OccurredAt     time.Time `json:"occurredAt"`
}

type reservationCanceledEvent struct {
	EventID        string    `json:"eventId"`
	ReservationUID string    `json:"reservationUid"`
	Username       string    `json:"username"`
	OccurredAt     time.Time `json:"occurredAt"`
}

func (s *server) consumeReservationCreated(ctx context.Context, cfg config.Config) {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  splitCSV(cfg.KafkaBrokers),
		Topic:    cfg.KafkaReservationCreatedTopic,
		GroupID:  cfg.KafkaGroupID + ".created",
		MinBytes: 1,
		MaxBytes: 10e6,
	})
	defer reader.Close()

	for {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			log.Printf("loyalty reservation.created kafka read failed: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		var event reservationCreatedEvent
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			log.Printf("invalid reservation created event: %v", err)
			_ = reader.CommitMessages(ctx, msg)
			continue
		}
		if err := validateLoyaltyEvent(event.EventID, event.ReservationUID, event.Username); err != nil {
			log.Printf("invalid reservation created event: %v", err)
			_ = reader.CommitMessages(ctx, msg)
			continue
		}
		if err := s.applyReservationCreated(ctx, event); err != nil {
			log.Printf("failed to apply reservation created event %s: %v", event.EventID, err)
			continue
		}
		if err := reader.CommitMessages(ctx, msg); err != nil {
			log.Printf("failed to commit reservation created event %s: %v", event.EventID, err)
		}
	}
}

func (s *server) consumeReservationCanceled(ctx context.Context, cfg config.Config) {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  splitCSV(cfg.KafkaBrokers),
		Topic:    cfg.KafkaReservationCanceledTopic,
		GroupID:  cfg.KafkaGroupID + ".canceled",
		MinBytes: 1,
		MaxBytes: 10e6,
	})
	defer reader.Close()

	for {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			log.Printf("loyalty kafka read failed: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		var event reservationCanceledEvent
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			log.Printf("invalid reservation canceled event: %v", err)
			_ = reader.CommitMessages(ctx, msg)
			continue
		}
		if err := validateLoyaltyEvent(event.EventID, event.ReservationUID, event.Username); err != nil {
			log.Printf("invalid reservation canceled event: %v", err)
			_ = reader.CommitMessages(ctx, msg)
			continue
		}
		if err := s.applyReservationCanceled(ctx, event); err != nil {
			log.Printf("failed to apply reservation canceled event %s: %v", event.EventID, err)
			continue
		}
		if err := reader.CommitMessages(ctx, msg); err != nil {
			log.Printf("failed to commit reservation canceled event %s: %v", event.EventID, err)
		}
	}
}

func validateLoyaltyEvent(eventID, reservationUID, username string) error {
	if eventID == "" || reservationUID == "" || username == "" {
		return errors.New("missing eventId, reservationUid or username")
	}
	if _, err := uuid.Parse(eventID); err != nil {
		return errors.New("bad eventId")
	}
	if _, err := uuid.Parse(reservationUID); err != nil {
		return errors.New("bad reservationUid")
	}
	return nil
}

func (s *server) applyReservationCreated(ctx context.Context, event reservationCreatedEvent) error {
	return s.applyReservationLifecycle(ctx, event.EventID, event.ReservationUID, event.Username, true)
}

func (s *server) applyReservationCanceled(ctx context.Context, event reservationCanceledEvent) error {
	return s.applyReservationLifecycle(ctx, event.EventID, event.ReservationUID, event.Username, false)
}

func (s *server) applyReservationLifecycle(ctx context.Context, eventID, reservationUID, username string, active bool) error {
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

	if _, err := tx.ExecContext(ctx, `
INSERT INTO loyalty(username, reservation_count, status, discount)
VALUES ($1, 0, 'BRONZE', 5)
ON CONFLICT (username) DO NOTHING
`, username); err != nil {
		return err
	}

	delta, err := s.applyReservationState(ctx, tx, reservationUID, username, active)
	if err != nil {
		return err
	}
	if delta != 0 {
		if _, err := tx.ExecContext(ctx, `
UPDATE loyalty
SET reservation_count = GREATEST(reservation_count + $2, 0),
	status = CASE
		WHEN GREATEST(reservation_count + $2, 0) >= 20 THEN 'GOLD'
		WHEN GREATEST(reservation_count + $2, 0) >= 10 THEN 'SILVER'
		ELSE 'BRONZE'
	END,
	discount = CASE
		WHEN GREATEST(reservation_count + $2, 0) >= 20 THEN 10
		WHEN GREATEST(reservation_count + $2, 0) >= 10 THEN 7
		ELSE 5
	END
WHERE username = $1
`, username, delta); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO processed_events(event_id) VALUES ($1)`, eventID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *server) applyReservationState(ctx context.Context, tx *sql.Tx, reservationUID string, username string, active bool) (int, error) {
	var currentActive bool
	err := tx.QueryRowContext(ctx, `
SELECT active
FROM loyalty_reservations
WHERE reservation_uid = $1
FOR UPDATE
`, reservationUID).Scan(&currentActive)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO loyalty_reservations(reservation_uid, username, active)
VALUES ($1, $2, $3)
`, reservationUID, username, active); err != nil {
			return 0, err
		}
		if active {
			return 1, nil
		}
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if currentActive == active {
		return 0, nil
	}
	if !currentActive && active {
		return 0, nil
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE loyalty_reservations
SET username = $2, active = $3, updated_at = now()
WHERE reservation_uid = $1
`, reservationUID, username, active); err != nil {
		return 0, err
	}
	if active {
		return 1, nil
	}
	return -1, nil
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
