package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/gazizov-ai/rsoi-project/reservation/internal/domain"
	errs "github.com/gazizov-ai/rsoi-project/reservation/internal/errors"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type HotelRepository struct {
	db *sqlx.DB
}

func NewHotelRepository(db *sqlx.DB) *HotelRepository {
	return &HotelRepository{db: db}
}

func (r *HotelRepository) GetByID(ctx context.Context, id int64, startDate, endDate time.Time) (*domain.Hotel, error) {
	const op = "postgres.HotelRepository.GetByID"

	var row HotelRow
	err := r.db.GetContext(ctx, &row, `
SELECT h.id, h.hotel_uid, h.name, h.country, h.city, h.address, h.stars, h.price, h.total_rooms,
	COALESCE((
		SELECT COUNT(*)
		FROM reservations res
		WHERE res.hotel_id = h.id
			AND res.status = 'PAID'
			AND res.start_date < $3
			AND res.end_date > $2
	), 0) AS occupied_rooms
FROM hotels h
WHERE h.id = $1
`, id, startDate, endDate)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errs.E(op, errs.ErrHotelNotFound, err)
	}
	if err != nil {
		return nil, errs.E(op, errs.ErrReservationStorage, err)
	}
	hotel := hotelRowToDomain(row)
	return &hotel, nil
}

func (r *HotelRepository) GetByUID(ctx context.Context, uid uuid.UUID, startDate, endDate time.Time) (*domain.Hotel, error) {
	const op = "postgres.HotelRepository.GetByUID"

	var row HotelRow
	err := r.db.GetContext(ctx, &row, `
SELECT h.id, h.hotel_uid, h.name, h.country, h.city, h.address, h.stars, h.price, h.total_rooms,
	COALESCE((
		SELECT COUNT(*)
		FROM reservations res
		WHERE res.hotel_id = h.id
			AND res.status = 'PAID'
			AND res.start_date < $3
			AND res.end_date > $2
	), 0) AS occupied_rooms
FROM hotels h
WHERE h.hotel_uid = $1
`, uid, startDate, endDate)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errs.E(op, errs.ErrHotelNotFound, err)
	}
	if err != nil {
		return nil, errs.E(op, errs.ErrReservationStorage, err)
	}
	hotel := hotelRowToDomain(row)
	return &hotel, nil
}

func (r *HotelRepository) List(ctx context.Context, limit, offset int, startDate, endDate time.Time) ([]domain.Hotel, error) {
	const op = "postgres.HotelRepository.List"

	var rows []HotelRow
	err := r.db.SelectContext(ctx, &rows, `
SELECT h.id, h.hotel_uid, h.name, h.country, h.city, h.address, h.stars, h.price, h.total_rooms,
	COALESCE((
		SELECT COUNT(*)
		FROM reservations res
		WHERE res.hotel_id = h.id
			AND res.status = 'PAID'
			AND res.start_date < $2
			AND res.end_date > $1
	), 0) AS occupied_rooms
FROM hotels h
ORDER BY h.id
LIMIT $3 OFFSET $4
`, startDate, endDate, limit, offset)
	if err != nil {
		return nil, errs.E(op, errs.ErrReservationStorage, err)
	}
	hotels := make([]domain.Hotel, 0, len(rows))
	for _, row := range rows {
		hotels = append(hotels, hotelRowToDomain(row))
	}
	return hotels, nil
}

func (r *HotelRepository) Count(ctx context.Context) (int64, error) {
	const op = "postgres.HotelRepository.Count"

	var count int64
	if err := r.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM hotels`); err != nil {
		return 0, errs.E(op, errs.ErrReservationStorage, err)
	}
	return count, nil
}
