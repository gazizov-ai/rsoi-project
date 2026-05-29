package transport

import (
	"net/http"
	"strings"

	"github.com/gazizov-ai/rsoi-project/common/auth"
	"github.com/gazizov-ai/rsoi-project/gateway/config"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewRouter(cfg config.Config) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors)

	r.Get("/manage/health", Health)
	h := NewHandler(cfg)
	validator := auth.NewValidator(cfg.IdentityURL+"/api/v1/jwks", cfg.JWTIssuer)
	r.Get("/api/v1/authorize", h.Authorize)
	r.Get("/api/v1/callback", h.Callback)
	r.Get("/api/v1/hotels", h.ListHotels)
	r.Route("/api/v1", func(api chi.Router) {
		api.Use(validator.Middleware)
		api.Get("/loyalty", h.GetLoyalty)
		api.Get("/reservations", h.ListReservations)
		api.Post("/reservations", h.CreateReservation)
		api.Get("/reservations/{reservationUid}", h.GetReservation)
		api.Delete("/reservations/{reservationUid}", h.CancelReservation)
		api.Get("/me", h.Me)
		api.Get("/statistics", h.Statistics)
		api.Get("/users", h.ListUsers)
		api.Post("/users", h.CreateUser)
	})

	return r
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		if strings.EqualFold(r.Method, http.MethodOptions) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
