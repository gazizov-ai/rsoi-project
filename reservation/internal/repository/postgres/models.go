package postgres

import (
	"database/sql"

	"github.com/google/uuid"
)

type ReservationRow struct {
	ID            int64         `db:"id"`
	ReservationID uuid.UUID     `db:"reservation_uid"`
	Username      string        `db:"username"`
	PaymentUID    uuid.UUID     `db:"payment_uid"`
	HotelID       sql.NullInt64 `db:"hotel_id"`
	Status        string        `db:"status"`
	StartDate     sql.NullTime  `db:"start_date"`
	EndDate       sql.NullTime  `db:"end_date"`
}

type HotelRow struct {
	ID            int64         `db:"id"`
	HotelUID      uuid.UUID     `db:"hotel_uid"`
	Name          string        `db:"name"`
	Country       string        `db:"country"`
	City          string        `db:"city"`
	Address       string        `db:"address"`
	Stars         sql.NullInt64 `db:"stars"`
	Price         int64         `db:"price"`
	TotalRooms    int           `db:"total_rooms"`
	OccupiedRooms int           `db:"occupied_rooms"`
}
