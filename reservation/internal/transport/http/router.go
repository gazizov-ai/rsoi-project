package transport

import (
	"strings"

	"github.com/gazizov-ai/rsoi-project/common/auth"
	"github.com/gazizov-ai/rsoi-project/reservation/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewRouter(svc *service.Service, identityURL, issuer string) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/manage/health", Health)
	h := NewHandler(svc)
	validator := auth.NewValidator(strings.TrimRight(identityURL, "/")+"/api/v1/jwks", issuer)
	r.Get("/api/v1/hotels", h.ListHotels)
	r.Get("/api/v1/hotels/{hotelUid}", h.GetHotel)
	r.Route("/api/v1", func(api chi.Router) {
		api.Use(validator.Middleware)
		api.Get("/reservations", h.ListReservations)
		api.Post("/reservations", h.CreateReservation)
		api.Get("/reservations/{reservationUid}", h.GetReservation)
		api.Delete("/reservations/{reservationUid}", h.CancelReservation)
		api.Post("/reservations/{reservationUid}/cancel", h.CancelReservation)
	})

	return r
}
