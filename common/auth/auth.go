package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

type contextKey string

const claimsKey contextKey = "jwtClaims"

var (
	ErrNoToken      = errors.New("missing bearer token")
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("expired token")
	ErrForbidden    = errors.New("forbidden")
)

type Claims struct {
	Subject  string
	Username string
	Email    string
	Roles    []string
	Scopes   []string
	Issuer   string
	Expires  time.Time
	Raw      map[string]any
}

func (c Claims) HasRole(role string) bool {
	for _, current := range c.Roles {
		if strings.EqualFold(current, role) {
			return true
		}
	}
	return false
}

func FromContext(ctx context.Context) (Claims, bool) {
	claims, ok := ctx.Value(claimsKey).(Claims)
	return claims, ok
}

func ContextWithClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

type Validator struct {
	jwksURL string
	issuer  string
	client  *http.Client

	mu      sync.RWMutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
}

func NewValidator(jwksURL, issuer string) *Validator {
	return &Validator{
		jwksURL: jwksURL,
		issuer:  issuer,
		client:  &http.Client{Timeout: 5 * time.Second},
		keys:    make(map[string]*rsa.PublicKey),
	}
}

func (v *Validator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := v.ValidateRequest(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(ContextWithClaims(r.Context(), claims)))
	})
}

func (v *Validator) RequireRole(role string, next http.Handler) http.Handler {
	return v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, _ := FromContext(r.Context())
		if !claims.HasRole(role) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func (v *Validator) ValidateRequest(r *http.Request) (Claims, error) {
	token := BearerToken(r.Header.Get("Authorization"))
	if token == "" {
		return Claims{}, ErrNoToken
	}
	return v.Validate(token)
}

func BearerToken(header string) string {
	if header == "" {
		return ""
	}
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func (v *Validator) Validate(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrInvalidToken
	}

	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
		Type      string `json:"typ"`
	}
	if err := decodeSegment(parts[0], &header); err != nil {
		return Claims{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if header.Algorithm != "RS256" || header.KeyID == "" {
		return Claims{}, ErrInvalidToken
	}

	var payload map[string]any
	if err := decodeSegment(parts[1], &payload); err != nil {
		return Claims{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	key, err := v.key(header.KeyID)
	if err != nil {
		return Claims{}, err
	}

	signingInput := parts[0] + "." + parts[1]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	sum := sha256.Sum256([]byte(signingInput))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], signature); err != nil {
		return Claims{}, ErrInvalidToken
	}

	claims, err := claimsFromPayload(payload)
	if err != nil {
		return Claims{}, err
	}
	if v.issuer != "" && claims.Issuer != v.issuer {
		return Claims{}, ErrInvalidToken
	}
	if !claims.Expires.After(time.Now()) {
		return Claims{}, ErrExpiredToken
	}

	return claims, nil
}

func (v *Validator) key(kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	key := v.keys[kid]
	stale := time.Since(v.fetched) > 5*time.Minute
	v.mu.RUnlock()
	if key != nil && !stale {
		return key, nil
	}

	if err := v.fetchKeys(); err != nil {
		return nil, err
	}

	v.mu.RLock()
	defer v.mu.RUnlock()
	key = v.keys[kid]
	if key == nil {
		return nil, ErrInvalidToken
	}
	return key, nil
}

func (v *Validator) fetchKeys() error {
	resp, err := v.client.Get(v.jwksURL)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ErrInvalidToken
	}

	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Use string `json:"use"`
			Kid string `json:"kid"`
			Alg string `json:"alg"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, jwk := range jwks.Keys {
		if jwk.Kty != "RSA" || jwk.Kid == "" {
			continue
		}
		nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
		if err != nil {
			continue
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
		if err != nil {
			continue
		}
		e := 0
		for _, b := range eBytes {
			e = e<<8 + int(b)
		}
		if e == 0 {
			continue
		}
		keys[jwk.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}
	}

	v.mu.Lock()
	v.keys = keys
	v.fetched = time.Now()
	v.mu.Unlock()
	return nil
}

func decodeSegment(segment string, dst any) error {
	data, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

func claimsFromPayload(payload map[string]any) (Claims, error) {
	claims := Claims{Raw: payload}
	claims.Subject = stringClaim(payload, "sub")
	claims.Username = stringClaim(payload, "preferred_username")
	if claims.Username == "" {
		claims.Username = claims.Subject
	}
	claims.Email = stringClaim(payload, "email")
	claims.Issuer = stringClaim(payload, "iss")
	claims.Roles = stringSliceClaim(payload, "roles")
	claims.Scopes = strings.Fields(stringClaim(payload, "scope"))

	exp, ok := numericClaim(payload, "exp")
	if !ok {
		return Claims{}, ErrInvalidToken
	}
	claims.Expires = time.Unix(int64(exp), 0)
	return claims, nil
}

func stringClaim(payload map[string]any, name string) string {
	value, _ := payload[name].(string)
	return value
}

func numericClaim(payload map[string]any, name string) (float64, bool) {
	value, ok := payload[name].(float64)
	return value, ok
}

func stringSliceClaim(payload map[string]any, name string) []string {
	value, ok := payload[name]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return typed
	case string:
		if typed == "" {
			return nil
		}
		return []string{typed}
	default:
		return nil
	}
}
