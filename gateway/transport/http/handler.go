package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gazizov-ai/rsoi-project/common/auth"
	"github.com/gazizov-ai/rsoi-project/common/httputil"
	"github.com/gazizov-ai/rsoi-project/gateway/config"
	"github.com/gazizov-ai/rsoi-project/gateway/internal/events"
	"github.com/go-chi/chi/v5"
)

const (
	reservationServiceUnavailable = "Reservation Service unavailable"
	paymentServiceUnavailable     = "Payment Service unavailable"
	loyaltyServiceUnavailable     = "Loyalty Service unavailable"
	bronzeLoyaltyStatus           = "BRONZE"
	bronzeLoyaltyDiscount         = 5
)

type Handler struct {
	cfg       config.Config
	client    *http.Client
	publisher *events.Publisher
}

func NewHandler(cfg config.Config) *Handler {
	return &Handler{
		cfg:       cfg,
		client:    &http.Client{Timeout: 6 * time.Second},
		publisher: events.NewPublisher(cfg.KafkaBrokers, cfg.KafkaTopic),
	}
}

func Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
	w.Write([]byte("status: ok"))
}

func (h *Handler) Authorize(w http.ResponseWriter, r *http.Request) {
	state := strconv.FormatInt(time.Now().UnixNano(), 36)
	target, _ := url.Parse(h.cfg.IdentityPublicURL + "/api/v1/authorize")
	q := target.Query()
	q.Set("response_type", "code")
	q.Set("client_id", h.cfg.ClientID)
	q.Set("redirect_uri", h.cfg.RedirectURI)
	q.Set("scope", "openid profile email")
	q.Set("state", state)
	target.RawQuery = q.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		httputil.Error(w, http.StatusBadRequest, "missing code")
		return
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", h.cfg.ClientID)
	form.Set("client_secret", h.cfg.ClientSecret)
	form.Set("redirect_uri", h.cfg.RedirectURI)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.cfg.IdentityURL+"/api/v1/token", strings.NewReader(form.Encode()))
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to build token request")
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := h.client.Do(req)
	if err != nil {
		httputil.Error(w, http.StatusBadGateway, "identity provider is unavailable")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		httputil.Error(w, http.StatusUnauthorized, "failed to exchange code")
		return
	}
	var tokens tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		httputil.Error(w, http.StatusBadGateway, "invalid token response")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	target, _ := url.Parse(h.cfg.UIURL)
	fragment := url.Values{}
	fragment.Set("access_token", tokens.AccessToken)
	fragment.Set("id_token", tokens.IDToken)
	target.Fragment = fragment.Encode()
	redirectURL, err := json.Marshal(target.String())
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to build redirect")
		return
	}
	fmt.Fprintf(w, callbackPage, redirectURL)
}

func (h *Handler) ListHotels(w http.ResponseWriter, r *http.Request) {
	var out hotelPage
	status, err := h.do(r, http.MethodGet, h.cfg.ReservationURL+"/api/v1/hotels?"+r.URL.RawQuery, nil, &out)
	if err != nil || status >= 500 {
		httputil.Error(w, http.StatusInternalServerError, reservationServiceUnavailable)
		return
	}
	writeUpstream(w, status, out)
}

func (h *Handler) GetLoyalty(w http.ResponseWriter, r *http.Request) {
	var out loyaltyResponse
	status, err := h.do(r, http.MethodGet, h.cfg.LoyaltyURL+"/api/v1/loyalty", nil, &out)
	if err != nil || status >= 500 {
		claims, _ := auth.FromContext(r.Context())
		httputil.JSON(w, http.StatusOK, bronzeLoyalty(claims.Username))
		return
	}
	writeUpstream(w, status, out)
}

func (h *Handler) ListReservations(w http.ResponseWriter, r *http.Request) {
	reservations, status, err := h.reservations(r)
	if err != nil || status >= 500 {
		httputil.Error(w, http.StatusInternalServerError, reservationServiceUnavailable)
		return
	}
	if status != http.StatusOK {
		httputil.Error(w, status, "failed to list reservations")
		return
	}
	paymentAvailable := h.serviceAvailable(r.Context(), h.cfg.PaymentURL+"/manage/health")
	out := make([]reservationDetailsResponse, 0, len(reservations))
	for _, reservation := range reservations {
		out = append(out, h.withPayment(r, reservation, paymentAvailable))
	}
	httputil.JSON(w, http.StatusOK, out)
}

func (h *Handler) GetReservation(w http.ResponseWriter, r *http.Request) {
	var reservation reservationResponse
	status, err := h.do(r, http.MethodGet, h.cfg.ReservationURL+"/api/v1/reservations/"+chi.URLParam(r, "reservationUid"), nil, &reservation)
	if err != nil || status >= 500 {
		httputil.Error(w, http.StatusInternalServerError, reservationServiceUnavailable)
		return
	}
	if status != http.StatusOK {
		httputil.Error(w, status, "reservation not found")
		return
	}
	httputil.JSON(w, http.StatusOK, h.withPayment(r, reservation, true))
}

func (h *Handler) CreateReservation(w http.ResponseWriter, r *http.Request) {
	var req createReservationRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	start, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid startDate")
		return
	}
	end, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil || !end.After(start) {
		httputil.Error(w, http.StatusBadRequest, "invalid endDate")
		return
	}

	var hotel hotelResponse
	hotelURL := h.cfg.ReservationURL + "/api/v1/hotels/" + url.PathEscape(req.HotelUID)
	hotelQuery := url.Values{}
	hotelQuery.Set("startDate", req.StartDate)
	hotelQuery.Set("endDate", req.EndDate)
	hotelURL += "?" + hotelQuery.Encode()
	status, err := h.do(r, http.MethodGet, hotelURL, nil, &hotel)
	if err != nil || status >= 500 {
		httputil.Error(w, http.StatusServiceUnavailable, reservationServiceUnavailable)
		return
	}
	if status != http.StatusOK {
		httputil.Error(w, status, "hotel not found")
		return
	}
	if hotel.AvailableRooms <= 0 {
		httputil.Error(w, http.StatusConflict, "No rooms available")
		return
	}

	var loyalty loyaltyResponse
	status, err = h.do(r, http.MethodGet, h.cfg.LoyaltyURL+"/api/v1/loyalty", nil, &loyalty)
	loyaltyAvailable := err == nil && status == http.StatusOK
	if !loyaltyAvailable {
		claims, _ := auth.FromContext(r.Context())
		loyalty = bronzeLoyalty(claims.Username)
	}
	nights := int(end.Sub(start).Hours() / 24)
	price := int(hotel.Price) * nights * (100 - loyalty.Discount) / 100

	var payment paymentResponse
	status, err = h.do(r, http.MethodPost, h.cfg.PaymentURL+"/api/v1/payments", map[string]int{"price": price}, &payment)
	if err != nil || status != http.StatusCreated {
		httputil.Error(w, http.StatusServiceUnavailable, paymentServiceUnavailable)
		return
	}

	var reservation reservationResponse
	createReq := map[string]string{
		"hotelUid":   req.HotelUID,
		"paymentUid": payment.PaymentUID,
		"startDate":  req.StartDate,
		"endDate":    req.EndDate,
	}
	status, err = h.do(r, http.MethodPost, h.cfg.ReservationURL+"/api/v1/reservations", createReq, &reservation)
	if err != nil || status != http.StatusCreated {
		h.cancelPayment(r, payment.PaymentUID)
		if status == http.StatusConflict {
			httputil.Error(w, http.StatusConflict, "No rooms available")
			return
		}
		httputil.Error(w, http.StatusServiceUnavailable, reservationServiceUnavailable)
		return
	}

	if loyaltyAvailable {
		var updated loyaltyResponse
		status, err = h.do(r, http.MethodPost, h.cfg.LoyaltyURL+"/api/v1/loyalty/increase", nil, &updated)
		if err != nil || status != http.StatusOK {
			h.cancelReservation(r, reservation.ReservationUID)
			h.cancelPayment(r, payment.PaymentUID)
			httputil.Error(w, http.StatusServiceUnavailable, loyaltyServiceUnavailable)
			return
		}
	}
	h.publishEvent(r, "reservation.created", map[string]any{"reservationUid": reservation.ReservationUID, "paymentUid": payment.PaymentUID, "price": payment.Price})

	httputil.JSON(w, http.StatusOK, createReservationResponse{
		ReservationUID: reservation.ReservationUID,
		HotelUID:       req.HotelUID,
		StartDate:      reservation.StartDate,
		EndDate:        reservation.EndDate,
		Discount:       loyalty.Discount,
		Status:         reservation.Status,
		Payment:        &payment,
	})
}

func (h *Handler) CancelReservation(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "reservationUid")
	var reservation reservationResponse
	status, err := h.do(r, http.MethodGet, h.cfg.ReservationURL+"/api/v1/reservations/"+url.PathEscape(uid), nil, &reservation)
	if err != nil || status >= 500 {
		httputil.Error(w, http.StatusInternalServerError, reservationServiceUnavailable)
		return
	}
	if status != http.StatusOK {
		httputil.Error(w, http.StatusNotFound, "reservation not found")
		return
	}

	status, err = h.do(r, http.MethodDelete, h.cfg.ReservationURL+"/api/v1/reservations/"+url.PathEscape(uid), nil, nil)
	if err != nil || status >= 500 {
		httputil.Error(w, http.StatusInternalServerError, reservationServiceUnavailable)
		return
	}
	if status >= 300 {
		httputil.Error(w, http.StatusBadGateway, "failed to cancel reservation")
		return
	}

	status, err = h.cancelPayment(r, reservation.PaymentUID)
	if err != nil || status >= 500 {
		h.retryPaymentCancel(r.Header.Get("Authorization"), reservation.PaymentUID)
	} else if status != http.StatusNotFound && status >= 300 {
		h.retryPaymentCancel(r.Header.Get("Authorization"), reservation.PaymentUID)
	}

	var loyalty loyaltyResponse
	status, err = h.do(r, http.MethodPost, h.cfg.LoyaltyURL+"/api/v1/loyalty/decrease", nil, &loyalty)
	if err != nil || status >= 500 {
		h.retryLoyaltyDecrease(r.Header.Get("Authorization"))
	}
	h.publishEvent(r, "reservation.canceled", map[string]any{"reservationUid": reservation.ReservationUID, "paymentUid": reservation.PaymentUID})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.FromContext(r.Context())
	reservations, status, err := h.reservations(r)
	reservationsUnavailable := false
	if err != nil || status >= 500 {
		reservations = []reservationResponse{}
		reservationsUnavailable = true
	}
	var loyalty loyaltyResponse
	var loyaltyPayload any = bronzeLoyalty(claims.Username)
	status, err = h.do(r, http.MethodGet, h.cfg.LoyaltyURL+"/api/v1/loyalty", nil, &loyalty)
	if err == nil && status == http.StatusOK {
		loyaltyPayload = loyalty
	}
	paymentAvailable := h.serviceAvailable(r.Context(), h.cfg.PaymentURL+"/manage/health")
	out := make([]reservationDetailsResponse, 0, len(reservations))
	for _, reservation := range reservations {
		out = append(out, h.withPayment(r, reservation, paymentAvailable))
	}
	httputil.JSON(w, http.StatusOK, map[string]any{
		"username":                claims.Username,
		"email":                   claims.Email,
		"roles":                   claims.Roles,
		"reservations":            out,
		"reservationsUnavailable": reservationsUnavailable,
		"loyalty":                 loyaltyPayload,
	})
}

func (h *Handler) Statistics(w http.ResponseWriter, r *http.Request) {
	var out any
	status, err := h.do(r, http.MethodGet, h.cfg.StatisticsURL+"/api/v1/statistics", nil, &out)
	if err != nil {
		httputil.Error(w, http.StatusBadGateway, "statistics service is unavailable")
		return
	}
	writeUpstream(w, status, out)
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	var out any
	status, err := h.do(r, http.MethodGet, h.cfg.IdentityURL+"/api/v1/users", nil, &out)
	if err != nil {
		httputil.Error(w, http.StatusBadGateway, "identity service is unavailable")
		return
	}
	writeUpstream(w, status, out)
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	var out any
	status, err := h.do(r, http.MethodPost, h.cfg.IdentityURL+"/api/v1/users", req, &out)
	if err != nil {
		httputil.Error(w, http.StatusBadGateway, "identity service is unavailable")
		return
	}
	writeUpstream(w, status, out)
}

func (h *Handler) reservations(r *http.Request) ([]reservationResponse, int, error) {
	var reservations []reservationResponse
	status, err := h.do(r, http.MethodGet, h.cfg.ReservationURL+"/api/v1/reservations", nil, &reservations)
	return reservations, status, err
}

func (h *Handler) withPayment(r *http.Request, reservation reservationResponse, paymentAvailable bool) reservationDetailsResponse {
	out := reservationDetailsResponse{
		ReservationUID: reservation.ReservationUID,
		Hotel:          hotelDetails(reservation.Hotel),
		StartDate:      reservation.StartDate,
		EndDate:        reservation.EndDate,
		Status:         reservation.Status,
	}
	if !paymentAvailable {
		out.PaymentUnavailable = true
		return out
	}
	var payment paymentResponse
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	status, err := h.doWithAuth(ctx, r.Header.Get("Authorization"), http.MethodGet, h.cfg.PaymentURL+"/api/v1/payments/"+url.PathEscape(reservation.PaymentUID), nil, &payment)
	if err == nil && status == http.StatusOK {
		out.Payment = &payment
	} else if err != nil || status >= 500 {
		out.PaymentUnavailable = true
	}
	return out
}

func hotelDetails(hotel hotelResponse) reservationHotelResponse {
	return reservationHotelResponse{
		HotelUID:    hotel.HotelUID,
		Name:        hotel.Name,
		FullAddress: fullAddress(hotel),
		Stars:       hotel.Stars,
	}
}

func fullAddress(hotel hotelResponse) string {
	parts := make([]string, 0, 3)
	for _, part := range []string{hotel.Country, hotel.City, hotel.Address} {
		if strings.TrimSpace(part) != "" {
			parts = append(parts, strings.TrimSpace(part))
		}
	}
	return strings.Join(parts, ", ")
}

func (h *Handler) cancelPayment(r *http.Request, uid string) (int, error) {
	if uid == "" {
		return http.StatusNoContent, nil
	}
	return h.do(r, http.MethodDelete, h.cfg.PaymentURL+"/api/v1/payments/"+url.PathEscape(uid), nil, nil)
}

func (h *Handler) cancelReservation(r *http.Request, uid string) {
	if uid == "" {
		return
	}
	_, _ = h.do(r, http.MethodDelete, h.cfg.ReservationURL+"/api/v1/reservations/"+url.PathEscape(uid), nil, nil)
}

func (h *Handler) publishEvent(r *http.Request, typ string, payload map[string]any) {
	claims, _ := auth.FromContext(r.Context())
	event := events.Event{Type: typ, Username: claims.Username, Payload: payload}
	_ = h.publisher.Publish(r.Context(), event)
	body := map[string]any{"type": typ, "username": claims.Username, "payload": payload}
	_, _ = h.do(r, http.MethodPost, h.cfg.StatisticsURL+"/api/v1/events", body, nil)
}

func (h *Handler) retryLoyaltyDecrease(authHeader string) {
	if authHeader == "" {
		return
	}
	go func() {
		deadline := time.Now().Add(30 * time.Second)
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			var out loyaltyResponse
			status, err := h.doWithAuth(ctx, authHeader, http.MethodPost, h.cfg.LoyaltyURL+"/api/v1/loyalty/decrease", nil, &out)
			cancel()
			if err == nil && status == http.StatusOK {
				return
			}
			if time.Now().After(deadline) {
				return
			}
			time.Sleep(2 * time.Second)
		}
	}()
}

func (h *Handler) retryPaymentCancel(authHeader string, paymentUID string) {
	if authHeader == "" || paymentUID == "" {
		return
	}
	go func() {
		deadline := time.Now().Add(45 * time.Second)
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			status, err := h.doWithAuth(ctx, authHeader, http.MethodDelete, h.cfg.PaymentURL+"/api/v1/payments/"+url.PathEscape(paymentUID), nil, nil)
			cancel()
			if err == nil && (status == http.StatusNoContent || status == http.StatusNotFound) {
				return
			}
			if time.Now().After(deadline) {
				return
			}
			time.Sleep(2 * time.Second)
		}
	}()
}

func (h *Handler) serviceAvailable(ctx context.Context, target string) bool {
	checkCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, target, nil)
	if err != nil {
		return false
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode >= 200 && resp.StatusCode < 500
}

func (h *Handler) do(r *http.Request, method, target string, body any, out any) (int, error) {
	return h.doWithAuth(r.Context(), r.Header.Get("Authorization"), method, target, body, out)
}

func (h *Handler) doWithAuth(ctx context.Context, authHeader, method, target string, body any, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", authHeader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if out != nil && resp.Body != nil && resp.StatusCode < 500 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil && err != io.EOF {
			return resp.StatusCode, err
		}
	} else {
		io.Copy(io.Discard, resp.Body)
	}
	return resp.StatusCode, nil
}

func writeUpstream(w http.ResponseWriter, status int, body any) {
	if status == 0 {
		status = http.StatusBadGateway
	}
	if status >= 400 {
		httputil.Error(w, status, http.StatusText(status))
		return
	}
	httputil.JSON(w, status, body)
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
}

type createReservationRequest struct {
	HotelUID  string `json:"hotelUid"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}

type createReservationResponse struct {
	ReservationUID string           `json:"reservationUid"`
	HotelUID       string           `json:"hotelUid"`
	StartDate      string           `json:"startDate"`
	EndDate        string           `json:"endDate"`
	Discount       int              `json:"discount"`
	Status         string           `json:"status"`
	Payment        *paymentResponse `json:"payment,omitempty"`
}

type hotelPage struct {
	Page          int             `json:"page"`
	PageSize      int             `json:"pageSize"`
	TotalElements int64           `json:"totalElements"`
	Items         []hotelResponse `json:"items"`
}

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

type loyaltyResponse struct {
	Username         string `json:"username"`
	ReservationCount int    `json:"reservationCount"`
	Status           string `json:"status"`
	Discount         int    `json:"discount"`
}

func bronzeLoyalty(username string) loyaltyResponse {
	return loyaltyResponse{
		Username:         username,
		ReservationCount: 0,
		Status:           bronzeLoyaltyStatus,
		Discount:         bronzeLoyaltyDiscount,
	}
}

type paymentResponse struct {
	PaymentUID string `json:"paymentUid"`
	Status     string `json:"status"`
	Price      int    `json:"price"`
}

type reservationResponse struct {
	ReservationUID string        `json:"reservationUid"`
	PaymentUID     string        `json:"paymentUid"`
	Status         string        `json:"status"`
	StartDate      string        `json:"startDate"`
	EndDate        string        `json:"endDate"`
	Hotel          hotelResponse `json:"hotel"`
}

type reservationHotelResponse struct {
	HotelUID    string `json:"hotelUid"`
	Name        string `json:"name"`
	FullAddress string `json:"fullAddress"`
	Stars       *int64 `json:"stars"`
}

type reservationDetailsResponse struct {
	ReservationUID     string                   `json:"reservationUid"`
	Hotel              reservationHotelResponse `json:"hotel"`
	StartDate          string                   `json:"startDate"`
	EndDate            string                   `json:"endDate"`
	Status             string                   `json:"status"`
	Payment            *paymentResponse         `json:"payment,omitempty"`
	PaymentUnavailable bool                     `json:"paymentUnavailable,omitempty"`
}

const callbackPage = `<!doctype html>
<html lang="ru">
<head><meta charset="utf-8"><title>RSOI Login</title></head>
<body>
<script>
window.location.replace(%s);
</script>
</body>
</html>`
