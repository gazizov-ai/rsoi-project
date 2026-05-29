package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/gazizov-ai/rsoi-project/reservation/internal/domain"
	errs "github.com/gazizov-ai/rsoi-project/reservation/internal/errors"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

type ReservationRepository struct {
	db *sqlx.DB
}

func NewReservationRepository(db *sqlx.DB) *ReservationRepository {
	return &ReservationRepository{db: db}
}

func (r *ReservationRepository) GetByReservationUID(ctx context.Context, uid uuid.UUID) (*domain.Reservation, error) {
	const op = "postgres.ReservationRepository.GetByReservationUID"

	var row ReservationRow

	query := `
		SELECT r.id, r.reservation_uid, r.username, r.payment_uid, r.hotel_id, r.status, r.start_date, r.end_date
		FROM reservations r
		WHERE r.reservation_uid = $1
	`

	err := r.db.GetContext(ctx, &row, query, uid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errs.E(op, errs.ErrReservationNotFound, err)
	}
	if err != nil {
		return nil, errs.E(op, errs.ErrReservationStorage, err)
	}

	reservation, err := reservationRowToDomain(row)
	if err != nil {
		return nil, errs.E(op, errs.ErrMapperConversion, err)
	}

	return &reservation, nil
}

func (r *ReservationRepository) ListByUsername(ctx context.Context, username string) ([]domain.Reservation, error) {
	const op = "postgres.ReservationRepository.ListByUsername"

	var rows []ReservationRow

	query := `
		SELECT r.id, r.reservation_uid, r.username, r.payment_uid, r.hotel_id, r.status, r.start_date, r.end_date
		FROM reservations r
		WHERE r.username = $1
		ORDER BY r.start_date DESC , r.id DESC
	`

	err := r.db.SelectContext(ctx, &rows, query, username)
	if err != nil {
		return nil, errs.E(op, errs.ErrReservationStorage, err)
	}
	reservations := make([]domain.Reservation, 0, len(rows))
	for _, row := range rows {
		reservation, err := reservationRowToDomain(row)
		if err != nil {
			return nil, errs.E(op, errs.ErrMapperConversion, err)
		}
		reservations = append(reservations, reservation)
	}
	return reservations, nil
}

func (repo *ReservationRepository) Create(ctx context.Context, r *domain.Reservation) (int64, error) {
	const op = "postgres.ReservationRepository.Create"

	if r.StartDate == nil || r.EndDate == nil {
		return 0, errs.E(op, errs.ErrCreateReservationFailed, errors.New("reservation dates are required"))
	}

	tx, err := repo.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, errs.E(op, errs.ErrCreateReservationFailed, err)
	}
	defer tx.Rollback()

	var totalRooms int
	err = tx.GetContext(ctx, &totalRooms, `
		SELECT total_rooms
		FROM hotels
		WHERE id = $1
		FOR UPDATE
	`, r.HotelID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errs.E(op, errs.ErrHotelNotFound, err)
	}
	if err != nil {
		return 0, errs.E(op, errs.ErrCreateReservationFailed, err)
	}

	var occupiedRooms int
	err = tx.GetContext(ctx, &occupiedRooms, `
		SELECT COUNT(*)
		FROM reservations
		WHERE hotel_id = $1
			AND status = 'PAID'
			AND start_date < $3
			AND end_date > $2
	`, r.HotelID, *r.StartDate, *r.EndDate)
	if err != nil {
		return 0, errs.E(op, errs.ErrCreateReservationFailed, err)
	}
	if occupiedRooms >= totalRooms {
		return 0, errs.E(op, errs.ErrNoRoomsAvailable, nil)
	}

	var id int64

	query := `
		INSERT INTO reservations (
			reservation_uid,
			username,
			payment_uid,
			hotel_id,
			status,
			start_date,
			end_date
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`

	err = tx.GetContext(ctx, &id, query,
		r.ReservationUID,
		r.Username,
		r.PaymentUID,
		r.HotelID,
		string(r.Status),
		r.StartDate,
		r.EndDate,
	)
	if err != nil {
		return 0, errs.E(op, errs.ErrCreateReservationFailed, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, errs.E(op, errs.ErrCreateReservationFailed, err)
	}

	return id, nil
}

func (r *ReservationRepository) UpdateStatus(ctx context.Context, uid uuid.UUID, status domain.ReservationStatus) error {
	const op = "postgres.ReservationRepository.UpdateStatus"

	query := `
		UPDATE reservations r
		SET status = $1
		WHERE r.reservation_uid = $2
	`

	result, err := r.db.ExecContext(ctx, query, string(status), uid)
	if err != nil {
		return errs.E(op, errs.ErrUpdateReservationStatusFailed, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errs.E(op, errs.ErrGetRowsAffectedFailed, err)
	}
	if affected == 0 {
		return errs.E(op, errs.ErrReservationNotFound, err)
	}

	return nil
}
