package transport

import (
	"time"

	"github.com/gazizov-ai/rsoi-project/reservation/internal/domain"
	"github.com/gazizov-ai/rsoi-project/reservation/internal/service"
)

type hotelResponse struct {
	HotelUID       string `json:"hotelUid"`
	Name           string `json:"name"`
	Country        string `json:"country"`
	City           string `json:"city"`
	Address        string `json:"address"`
	Stars          *int64 `json:"stars"`
	Price          int64  `json:"price"`
	TotalRooms     int    `json:"totalRooms"`
	OccupiedRooms  int    `json:"occupiedRooms"`
	AvailableRooms int    `json:"availableRooms"`
}

type reservationResponse struct {
	ReservationUID string        `json:"reservationUid"`
	PaymentUID     string        `json:"paymentUid"`
	Status         string        `json:"status"`
	StartDate      string        `json:"startDate"`
	EndDate        string        `json:"endDate"`
	Hotel          hotelResponse `json:"hotel"`
}

func hotelDTO(h domain.Hotel) hotelResponse {
	return hotelResponse{
		HotelUID:       h.UID.String(),
		Name:           h.Name,
		Country:        h.Country,
		City:           h.City,
		Address:        h.Address,
		Stars:          h.Stars,
		Price:          h.Price,
		TotalRooms:     h.TotalRooms,
		OccupiedRooms:  h.OccupiedRooms,
		AvailableRooms: h.AvailableRooms,
	}
}

func reservationDTO(view service.ReservationView) reservationResponse {
	return reservationResponse{
		ReservationUID: view.Reservation.ReservationUID.String(),
		PaymentUID:     view.Reservation.PaymentUID.String(),
		Status:         string(view.Reservation.Status),
		StartDate:      formatDate(view.Reservation.StartDate),
		EndDate:        formatDate(view.Reservation.EndDate),
		Hotel:          hotelDTO(view.Hotel),
	}
}

func formatDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}
