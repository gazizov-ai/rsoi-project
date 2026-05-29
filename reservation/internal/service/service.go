package service

import (
	"context"
	"errors"
	"time"

	"github.com/gazizov-ai/rsoi-project/reservation/internal/domain"
	errs "github.com/gazizov-ai/rsoi-project/reservation/internal/errors"
	"github.com/gazizov-ai/rsoi-project/reservation/internal/repository"
	"github.com/google/uuid"
)

type ReservationView struct {
	Reservation domain.Reservation
	Hotel       domain.Hotel
}

type Service struct {
	reservations repository.ReservationRepository
	hotels       repository.HotelRepository
}

func New(reservations repository.ReservationRepository, hotels repository.HotelRepository) *Service {
	return &Service{reservations: reservations, hotels: hotels}
}

func (s *Service) ListHotels(ctx context.Context, page, size int, startDate, endDate time.Time) ([]domain.Hotel, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 10
	}
	total, err := s.hotels.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	hotels, err := s.hotels.List(ctx, size, (page-1)*size, startDate, endDate)
	return hotels, total, err
}

func (s *Service) GetHotel(ctx context.Context, uid uuid.UUID, startDate, endDate time.Time) (*domain.Hotel, error) {
	return s.hotels.GetByUID(ctx, uid, startDate, endDate)
}

func (s *Service) CreateReservation(ctx context.Context, username string, hotelUID, paymentUID uuid.UUID, startDate, endDate time.Time) (ReservationView, error) {
	if !endDate.After(startDate) {
		return ReservationView{}, errors.New("endDate must be after startDate")
	}
	hotel, err := s.hotels.GetByUID(ctx, hotelUID, startDate, endDate)
	if err != nil {
		return ReservationView{}, err
	}
	reservation := &domain.Reservation{
		ReservationUID: uuid.New(),
		Username:       username,
		PaymentUID:     paymentUID,
		HotelID:        hotel.ID,
		Status:         domain.StatusPaid,
		StartDate:      &startDate,
		EndDate:        &endDate,
	}
	id, err := s.reservations.Create(ctx, reservation)
	if err != nil {
		return ReservationView{}, err
	}
	reservation.ID = id
	hotel, err = s.hotels.GetByID(ctx, hotel.ID, startDate, endDate)
	if err != nil {
		return ReservationView{}, err
	}
	return ReservationView{Reservation: *reservation, Hotel: *hotel}, nil
}

func (s *Service) ListReservations(ctx context.Context, username string) ([]ReservationView, error) {
	reservations, err := s.reservations.ListByUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	views := make([]ReservationView, 0, len(reservations))
	for _, reservation := range reservations {
		startDate, endDate := reservationAvailabilityWindow(reservation)
		hotel, err := s.hotels.GetByID(ctx, reservation.HotelID, startDate, endDate)
		if err != nil {
			return nil, err
		}
		views = append(views, ReservationView{Reservation: reservation, Hotel: *hotel})
	}
	return views, nil
}

func (s *Service) GetReservation(ctx context.Context, username string, uid uuid.UUID) (ReservationView, error) {
	reservation, err := s.reservations.GetByReservationUID(ctx, uid)
	if err != nil {
		return ReservationView{}, err
	}
	if reservation.Username != username {
		return ReservationView{}, errs.ErrReservationNotFound
	}
	startDate, endDate := reservationAvailabilityWindow(*reservation)
	hotel, err := s.hotels.GetByID(ctx, reservation.HotelID, startDate, endDate)
	if err != nil {
		return ReservationView{}, err
	}
	return ReservationView{Reservation: *reservation, Hotel: *hotel}, nil
}

func reservationAvailabilityWindow(reservation domain.Reservation) (time.Time, time.Time) {
	if reservation.StartDate != nil && reservation.EndDate != nil {
		return *reservation.StartDate, *reservation.EndDate
	}
	startDate := time.Now().Truncate(24 * time.Hour)
	return startDate, startDate.AddDate(0, 0, 1)
}

func (s *Service) CancelReservation(ctx context.Context, username string, uid uuid.UUID) error {
	if _, err := s.GetReservation(ctx, username, uid); err != nil {
		return err
	}
	return s.reservations.UpdateStatus(ctx, uid, domain.StatusCanceled)
}
