package main

import (
	"log"
	"net/http"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"

	"github.com/gazizov-ai/rsoi-project/reservation/internal/config"
	postgresrepo "github.com/gazizov-ai/rsoi-project/reservation/internal/repository/postgres"
	"github.com/gazizov-ai/rsoi-project/reservation/internal/service"
	transport "github.com/gazizov-ai/rsoi-project/reservation/internal/transport/http"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to read env: %v", err)
	}

	db, err := sqlx.Connect("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect postgres: %v", err)
	}
	defer db.Close()
	if err := migrate(db); err != nil {
		log.Fatalf("failed to migrate reservation db: %v", err)
	}

	reservations := postgresrepo.NewReservationRepository(db)
	hotels := postgresrepo.NewHotelRepository(db)
	svc := service.New(reservations, hotels)
	r := transport.NewRouter(svc, cfg.IdentityURL, cfg.JWTIssuer)

	srv := &http.Server{
		Addr:    cfg.Address(),
		Handler: r,
	}

	log.Printf("reservation listening on %s", cfg.Address())
	log.Fatal(srv.ListenAndServe())
}

func migrate(db *sqlx.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS hotels (
	id SERIAL PRIMARY KEY,
	hotel_uid uuid NOT NULL UNIQUE,
	name VARCHAR(255) NOT NULL,
	country VARCHAR(80) NOT NULL,
	city VARCHAR(80) NOT NULL,
	address VARCHAR(255) NOT NULL,
	stars INT,
	price INT NOT NULL,
	total_rooms INT NOT NULL DEFAULT 10
);

ALTER TABLE hotels
ADD COLUMN IF NOT EXISTS total_rooms INT NOT NULL DEFAULT 10;

CREATE TABLE IF NOT EXISTS reservations (
	id SERIAL PRIMARY KEY,
	reservation_uid uuid UNIQUE NOT NULL,
	username VARCHAR(80) NOT NULL,
	payment_uid uuid NOT NULL,
	hotel_id INT REFERENCES hotels(id),
	status VARCHAR(20) NOT NULL CHECK (status IN ('PAID', 'CANCELED')),
	start_date TIMESTAMPTZ,
	end_date TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO hotels(hotel_uid, name, country, city, address, stars, price, total_rooms)
VALUES
	('049161bb-badd-4fa8-9d90-87c9a82b0668', 'Ararat Park Hyatt Moscow', 'Россия', 'Москва', 'Неглинная ул., 4', 5, 10000, 10),
	('3b6c4f3b-4b0b-4f8d-a8f8-0e7ddf3995cb', 'Grand Hotel Europe', 'Россия', 'Санкт-Петербург', 'Михайловская ул., 1/7', 5, 8500, 8),
	('7a1f1f72-8b7e-47f4-9b29-a47cc7ee2ff8', 'Green Flow Rosa Khutor', 'Россия', 'Сочи', 'ул. Сулимовка, 9', 4, 6400, 6)
ON CONFLICT (hotel_uid) DO NOTHING;

DO $$
BEGIN
	IF EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_name = 'reservations' AND column_name = 'end_data'
	) AND NOT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_name = 'reservations' AND column_name = 'end_date'
	) THEN
		ALTER TABLE reservations RENAME COLUMN end_data TO end_date;
	END IF;
END $$;
`)
	return err
}
