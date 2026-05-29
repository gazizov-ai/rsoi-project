package transport

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gazizov-ai/rsoi-project/common/auth"
	"github.com/gazizov-ai/rsoi-project/common/httputil"
	errs "github.com/gazizov-ai/rsoi-project/reservation/internal/errors"
	"github.com/gazizov-ai/rsoi-project/reservation/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct {
	svc *service.Service
}

func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("status: ok"))
}

func (h *Handler) ListHotels(w http.ResponseWriter, r *http.Request) {
	page := intQuery(r, "page", 1)
	size := intQuery(r, "size", 10)
	startDate, endDate, err := availabilityDates(r)
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	hotels, total, err := h.svc.ListHotels(r.Context(), page, size, startDate, endDate)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to list hotels")
		return
	}
	items := make([]hotelResponse, 0, len(hotels))
	for _, hotel := range hotels {
		items = append(items, hotelDTO(hotel))
	}
	httputil.JSON(w, http.StatusOK, map[string]any{
		"page":          page,
		"pageSize":      size,
		"totalElements": total,
		"items":         items,
	})
}

func (h *Handler) GetHotel(w http.ResponseWriter, r *http.Request) {
	uid, err := uuid.Parse(chi.URLParam(r, "hotelUid"))
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid hotel uid")
		return
	}
	startDate, endDate, err := availabilityDates(r)
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	hotel, err := h.svc.GetHotel(r.Context(), uid, startDate, endDate)
	if errors.Is(err, errs.ErrHotelNotFound) {
		httputil.Error(w, http.StatusNotFound, "hotel not found")
		return
	}
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to get hotel")
		return
	}
	httputil.JSON(w, http.StatusOK, hotelDTO(*hotel))
}

func (h *Handler) ListReservations(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.FromContext(r.Context())
	views, err := h.svc.ListReservations(r.Context(), claims.Username)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to list reservations")
		return
	}
	resp := make([]reservationResponse, 0, len(views))
	for _, view := range views {
		resp = append(resp, reservationDTO(view))
	}
	httputil.JSON(w, http.StatusOK, resp)
}

func (h *Handler) GetReservation(w http.ResponseWriter, r *http.Request) {
	uid, err := uuid.Parse(chi.URLParam(r, "reservationUid"))
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid reservation uid")
		return
	}
	claims, _ := auth.FromContext(r.Context())
	view, err := h.svc.GetReservation(r.Context(), claims.Username, uid)
	if errors.Is(err, errs.ErrReservationNotFound) {
		httputil.Error(w, http.StatusNotFound, "reservation not found")
		return
	}
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to get reservation")
		return
	}
	httputil.JSON(w, http.StatusOK, reservationDTO(view))
}

func (h *Handler) CreateReservation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		HotelUID   string `json:"hotelUid"`
		PaymentUID string `json:"paymentUid"`
		StartDate  string `json:"startDate"`
		EndDate    string `json:"endDate"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	hotelUID, err := uuid.Parse(req.HotelUID)
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid hotel uid")
		return
	}
	paymentUID, err := uuid.Parse(req.PaymentUID)
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid payment uid")
		return
	}
	startDate, err := parseDate(req.StartDate)
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid startDate")
		return
	}
	endDate, err := parseDate(req.EndDate)
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid endDate")
		return
	}
	claims, _ := auth.FromContext(r.Context())
	view, err := h.svc.CreateReservation(r.Context(), claims.Username, hotelUID, paymentUID, startDate, endDate)
	if errors.Is(err, errs.ErrHotelNotFound) {
		httputil.Error(w, http.StatusNotFound, "hotel not found")
		return
	}
	if errors.Is(err, errs.ErrNoRoomsAvailable) {
		httputil.Error(w, http.StatusConflict, "No rooms available")
		return
	}
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httputil.JSON(w, http.StatusCreated, reservationDTO(view))
}

func (h *Handler) CancelReservation(w http.ResponseWriter, r *http.Request) {
	uid, err := uuid.Parse(chi.URLParam(r, "reservationUid"))
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid reservation uid")
		return
	}
	claims, _ := auth.FromContext(r.Context())
	err = h.svc.CancelReservation(r.Context(), claims.Username, uid)
	if errors.Is(err, errs.ErrReservationNotFound) {
		httputil.Error(w, http.StatusNotFound, "reservation not found")
		return
	}
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to cancel reservation")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func intQuery(r *http.Request, key string, fallback int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func parseDate(raw string) (time.Time, error) {
	return time.Parse("2006-01-02", raw)
}

func availabilityDates(r *http.Request) (time.Time, time.Time, error) {
	startRaw := r.URL.Query().Get("startDate")
	endRaw := r.URL.Query().Get("endDate")
	if startRaw == "" && endRaw == "" {
		now := time.Now()
		startDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return startDate, startDate.AddDate(0, 0, 1), nil
	}
	if startRaw == "" || endRaw == "" {
		return time.Time{}, time.Time{}, errors.New("startDate and endDate must be provided together")
	}
	startDate, err := parseDate(startRaw)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("invalid startDate")
	}
	endDate, err := parseDate(endRaw)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("invalid endDate")
	}
	if !endDate.After(startDate) {
		return time.Time{}, time.Time{}, errors.New("endDate must be after startDate")
	}
	return startDate, endDate, nil
}
