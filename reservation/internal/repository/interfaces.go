package repository

import (
	"context"
	"time"

	"github.com/gazizov-ai/rsoi-project/reservation/internal/domain"
	"github.com/google/uuid"
)

type ReservationRepository interface {
	Create(ctx context.Context, r *domain.Reservation) (int64, error)
	GetByReservationUID(ctx context.Context, uid uuid.UUID) (*domain.Reservation, error)
	ListByUsername(ctx context.Context, username string) ([]domain.Reservation, error)
	UpdateStatus(ctx context.Context, uid uuid.UUID, status domain.ReservationStatus) error
}

type HotelRepository interface {
	GetByID(ctx context.Context, id int64, startDate, endDate time.Time) (*domain.Hotel, error)
	GetByUID(ctx context.Context, uid uuid.UUID, startDate, endDate time.Time) (*domain.Hotel, error)
	List(ctx context.Context, limit, offset int, startDate, endDate time.Time) ([]domain.Hotel, error)
	Count(ctx context.Context) (int64, error)
}
