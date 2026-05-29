package domain

import (
	"time"

	"github.com/google/uuid"
)

type ReservationStatus string

const (
	StatusPaid     ReservationStatus = "PAID"
	StatusPending  ReservationStatus = "PENDING"
	StatusCanceled ReservationStatus = "CANCELED"
)

type Reservation struct {
	ID             int64
	ReservationUID uuid.UUID
	Username       string
	PaymentUID     uuid.UUID
	HotelID        int64
	Status         ReservationStatus
	StartDate      *time.Time
	EndDate        *time.Time
}
