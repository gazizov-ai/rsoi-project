package postgres

import (
	"errors"
	"fmt"
	"time"

	"github.com/gazizov-ai/rsoi-project/reservation/internal/domain"
)

func reservationRowToDomain(row ReservationRow) (domain.Reservation, error) {
	var hotelID int64
	if !row.HotelID.Valid {
		return domain.Reservation{}, errors.New("reservation row has null hotel")
	}
	hotelID = row.HotelID.Int64

	var status domain.ReservationStatus
	if row.Status != "PAID" && row.Status != "CANCELED" {
		return domain.Reservation{}, fmt.Errorf("reservation row has invalid status: %s", row.Status)
	}
	status = domain.ReservationStatus(row.Status)

	var startDate *time.Time
	if row.StartDate.Valid {
		t := row.StartDate.Time
		startDate = &t
	}

	var endDate *time.Time
	if row.EndDate.Valid {
		t := row.EndDate.Time
		endDate = &t
	}
	return domain.Reservation{
		ID:             row.ID,
		ReservationUID: row.ReservationID,
		Username:       row.Username,
		PaymentUID:     row.PaymentUID,
		HotelID:        hotelID,
		Status:         status,
		StartDate:      startDate,
		EndDate:        endDate,
	}, nil
}

func hotelRowToDomain(row HotelRow) domain.Hotel {
	var stars *int64
	if row.Stars.Valid {
		t := row.Stars.Int64
		stars = &t
	}
	availableRooms := row.TotalRooms - row.OccupiedRooms
	if availableRooms < 0 {
		availableRooms = 0
	}
	return domain.Hotel{
		ID:             row.ID,
		UID:            row.HotelUID,
		Name:           row.Name,
		Country:        row.Country,
		City:           row.City,
		Address:        row.Address,
		Stars:          stars,
		Price:          row.Price,
		TotalRooms:     row.TotalRooms,
		OccupiedRooms:  row.OccupiedRooms,
		AvailableRooms: availableRooms,
	}
}
