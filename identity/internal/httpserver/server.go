package httpserver

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gazizov-ai/rsoi-project/common/auth"
	"github.com/gazizov-ai/rsoi-project/common/httputil"
	"github.com/gazizov-ai/rsoi-project/identity/internal/config"
)

const keyID = "rsoi-identity-key"

type Server struct {
	db        *sql.DB
	cfg       config.Config
	private   *rsa.PrivateKey
	validator *auth.Validator
}

type user struct {
	ID           int64
	Username     string
	Email        string
	PasswordHash string
	Role         string
}

func New(db *sql.DB, cfg config.Config) (*Server, error) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	s := &Server{
		db:        db,
		cfg:       cfg,
		private:   private,
		validator: auth.NewValidator(strings.TrimRight(cfg.Issuer, "/")+"/api/v1/jwks", cfg.Issuer),
	}
	if err := s.migrate(context.Background()); err != nil {
		return nil, err
	}
	if err := s.seed(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /manage/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("status: ok"))
	})
	mux.HandleFunc("GET /.well-known/openid-configuration", s.discovery)
	mux.HandleFunc("GET /api/v1/jwks", s.jwks)
	mux.HandleFunc("GET /api/v1/authorize", s.authorize)
	mux.HandleFunc("POST /api/v1/login", s.login)
	mux.HandleFunc("POST /api/v1/register", s.register)
	mux.HandleFunc("POST /api/v1/token", s.token)
	mux.HandleFunc("POST /oauth/token", s.passwordToken)
	mux.Handle("GET /api/v1/userinfo", s.validator.Middleware(http.HandlerFunc(s.userinfo)))
	mux.Handle("GET /api/v1/users", s.validator.RequireRole("Admin", http.HandlerFunc(s.listUsers)))
	mux.Handle("POST /api/v1/users", s.validator.RequireRole("Admin", http.HandlerFunc(s.createUser)))
	return withCORS(mux)
}

func (s *Server) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS users (
	id SERIAL PRIMARY KEY,
	username VARCHAR(80) NOT NULL UNIQUE,
	email VARCHAR(255) NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	role VARCHAR(32) NOT NULL CHECK (role IN ('Admin', 'User')),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS oauth_clients (
	client_id VARCHAR(120) PRIMARY KEY,
	client_secret TEXT NOT NULL DEFAULT '',
	redirect_uri TEXT NOT NULL,
	name VARCHAR(255) NOT NULL DEFAULT 'RSOI SPA'
);

CREATE TABLE IF NOT EXISTS auth_codes (
	code TEXT PRIMARY KEY,
	username VARCHAR(80) NOT NULL REFERENCES users(username) ON DELETE CASCADE,
	client_id VARCHAR(120) NOT NULL REFERENCES oauth_clients(client_id) ON DELETE CASCADE,
	redirect_uri TEXT NOT NULL,
	scope TEXT NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL,
	used BOOLEAN NOT NULL DEFAULT false
);
`)
	return err
}

func (s *Server) seed(ctx context.Context) error {
	if err := s.seedUser(ctx, s.cfg.AdminUsername, s.cfg.AdminEmail, s.cfg.AdminPassword, "Admin"); err != nil {
		return err
	}
	if s.cfg.DefaultUserUsername != "" {
		if err := s.seedUser(ctx, s.cfg.DefaultUserUsername, s.cfg.DefaultUserEmail, s.cfg.DefaultUserPassword, "User"); err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO oauth_clients(client_id, client_secret, redirect_uri, name)
VALUES ($1, '', $2, 'RSOI SPA')
ON CONFLICT (client_id) DO UPDATE SET redirect_uri = EXCLUDED.redirect_uri
`, s.cfg.DefaultClientID, s.cfg.DefaultRedirect)
	return err
}

func (s *Server) seedUser(ctx context.Context, username, email, password, role string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO users(username, email, password_hash, role)
VALUES ($1, $2, $3, $4)
ON CONFLICT (username) DO NOTHING
`, username, email, hash, role)
	return err
}

func (s *Server) discovery(w http.ResponseWriter, r *http.Request) {
	issuer := strings.TrimRight(s.cfg.Issuer, "/")
	httputil.JSON(w, http.StatusOK, map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + "/api/v1/authorize",
		"token_endpoint":                        issuer + "/api/v1/token",
		"userinfo_endpoint":                     issuer + "/api/v1/userinfo",
		"jwks_uri":                              issuer + "/api/v1/jwks",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email"},
	})
}

func (s *Server) jwks(w http.ResponseWriter, r *http.Request) {
	pub := s.private.PublicKey
	e := big.NewInt(int64(pub.E)).Bytes()
	httputil.JSON(w, http.StatusOK, map[string]any{
		"keys": []map[string]string{
			{
				"kty": "RSA",
				"use": "sig",
				"kid": keyID,
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(e),
			},
		},
	})
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("response_type") != "code" || q.Get("client_id") == "" || q.Get("redirect_uri") == "" {
		httputil.Error(w, http.StatusBadRequest, "invalid authorization request")
		return
	}
	if err := s.validateClient(r.Context(), q.Get("client_id"), q.Get("redirect_uri")); err != nil {
		httputil.Error(w, http.StatusBadRequest, "unknown client or redirect uri")
		return
	}
	scope := q.Get("scope")
	if !strings.Contains(" "+scope+" ", " openid ") {
		scope = strings.TrimSpace("openid " + scope)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := strings.NewReplacer(
		"{{CLIENT_ID}}", html.EscapeString(q.Get("client_id")),
		"{{REDIRECT_URI}}", html.EscapeString(q.Get("redirect_uri")),
		"{{SCOPE}}", html.EscapeString(scope),
		"{{STATE}}", html.EscapeString(q.Get("state")),
	).Replace(loginPage)
	_, _ = w.Write([]byte(page))
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid form")
		return
	}
	clientID := r.Form.Get("client_id")
	redirectURI := r.Form.Get("redirect_uri")
	scope := r.Form.Get("scope")
	state := r.Form.Get("state")
	if err := s.validateClient(r.Context(), clientID, redirectURI); err != nil {
		httputil.Error(w, http.StatusBadRequest, "unknown client or redirect uri")
		return
	}

	u, err := s.userByUsername(r.Context(), r.Form.Get("username"))
	if err != nil || !checkPassword(u.PasswordHash, r.Form.Get("password")) {
		httputil.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if err := s.redirectWithCode(w, r, u.Username, clientID, redirectURI, scope, state); err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to create authorization code")
		return
	}
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid form")
		return
	}
	clientID := r.Form.Get("client_id")
	redirectURI := r.Form.Get("redirect_uri")
	scope := r.Form.Get("scope")
	state := r.Form.Get("state")
	if err := s.validateClient(r.Context(), clientID, redirectURI); err != nil {
		httputil.Error(w, http.StatusBadRequest, "unknown client or redirect uri")
		return
	}

	username := strings.TrimSpace(r.Form.Get("username"))
	email := strings.TrimSpace(r.Form.Get("email"))
	password := r.Form.Get("password")
	if username == "" || email == "" || password == "" {
		s.renderAuthError(w, http.StatusBadRequest, "Заполните логин, email и пароль")
		return
	}
	if _, err := s.insertUser(r.Context(), username, email, password, "User"); err != nil {
		s.renderAuthError(w, http.StatusConflict, "Пользователь с таким логином или email уже существует")
		return
	}
	if err := s.redirectWithCode(w, r, username, clientID, redirectURI, scope, state); err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to create authorization code")
		return
	}
}

func (s *Server) redirectWithCode(w http.ResponseWriter, r *http.Request, username, clientID, redirectURI, scope, state string) error {
	code, err := randomToken(32)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(r.Context(), `
INSERT INTO auth_codes(code, username, client_id, redirect_uri, scope, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
`, code, username, clientID, redirectURI, normalizeScope(scope), time.Now().Add(s.cfg.AuthCodeTTL))
	if err != nil {
		return err
	}

	target, _ := url.Parse(redirectURI)
	q := target.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	target.RawQuery = q.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
	return nil
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid form")
		return
	}
	if r.Form.Get("grant_type") != "authorization_code" {
		httputil.Error(w, http.StatusBadRequest, "unsupported grant_type")
		return
	}

	code := r.Form.Get("code")
	clientID := r.Form.Get("client_id")
	redirectURI := r.Form.Get("redirect_uri")

	var username, storedClientID, storedRedirectURI, scope string
	var expiresAt time.Time
	var used bool
	err := s.db.QueryRowContext(r.Context(), `
SELECT username, client_id, redirect_uri, scope, expires_at, used
FROM auth_codes
WHERE code = $1
`, code).Scan(&username, &storedClientID, &storedRedirectURI, &scope, &expiresAt, &used)
	if errors.Is(err, sql.ErrNoRows) {
		httputil.Error(w, http.StatusBadRequest, "invalid code")
		return
	}
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to read authorization code")
		return
	}
	if used || time.Now().After(expiresAt) || storedClientID != clientID || storedRedirectURI != redirectURI {
		httputil.Error(w, http.StatusBadRequest, "invalid code")
		return
	}
	if err := s.validateClient(r.Context(), clientID, redirectURI); err != nil {
		httputil.Error(w, http.StatusBadRequest, "unknown client or redirect uri")
		return
	}

	u, err := s.userByUsername(r.Context(), username)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to read user")
		return
	}
	_, _ = s.db.ExecContext(r.Context(), `UPDATE auth_codes SET used = true WHERE code = $1`, code)

	accessToken, err := s.signJWT(u, clientID, scope)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to sign token")
		return
	}
	idToken, err := s.signJWT(u, clientID, scope)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to sign token")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{
		"access_token": accessToken,
		"id_token":     idToken,
		"token_type":   "Bearer",
		"expires_in":   int(s.cfg.AccessTokenTTL.Seconds()),
		"scope":        scope,
	})
}

func (s *Server) passwordToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid form")
		return
	}
	if r.Form.Get("grant_type") != "password" {
		httputil.Error(w, http.StatusBadRequest, "unsupported grant_type")
		return
	}
	clientID := r.Form.Get("client_id")
	if clientID == "" {
		clientID = s.cfg.DefaultClientID
	}
	if err := s.validateClientSecret(r.Context(), clientID, r.Form.Get("client_secret")); err != nil {
		httputil.Error(w, http.StatusUnauthorized, "invalid client")
		return
	}
	scope := r.Form.Get("scope")
	u, err := s.userByUsername(r.Context(), r.Form.Get("username"))
	if err != nil || !checkPassword(u.PasswordHash, r.Form.Get("password")) {
		httputil.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	accessToken, err := s.signJWT(u, clientID, scope)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to sign token")
		return
	}
	idToken, err := s.signJWT(u, clientID, scope)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to sign token")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{
		"access_token": accessToken,
		"id_token":     idToken,
		"token_type":   "Bearer",
		"expires_in":   int(s.cfg.AccessTokenTTL.Seconds()),
		"scope":        normalizeScope(scope),
	})
}

func (s *Server) userinfo(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.FromContext(r.Context())
	httputil.JSON(w, http.StatusOK, map[string]any{
		"sub":                claims.Subject,
		"preferred_username": claims.Username,
		"email":              claims.Email,
		"roles":              claims.Roles,
	})
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, username, email, role FROM users ORDER BY id`)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	defer rows.Close()

	var users []map[string]any
	for rows.Next() {
		var id int64
		var username, email, role string
		if err := rows.Scan(&id, &username, &email, &role); err != nil {
			httputil.Error(w, http.StatusInternalServerError, "failed to scan user")
			return
		}
		users = append(users, map[string]any{"id": id, "username": username, "email": email, "role": role})
	}
	httputil.JSON(w, http.StatusOK, users)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)
	if req.Role == "" {
		req.Role = "User"
	}
	if req.Username == "" || req.Email == "" || req.Password == "" || (req.Role != "User" && req.Role != "Admin") {
		httputil.Error(w, http.StatusBadRequest, "invalid user")
		return
	}
	id, err := s.insertUser(r.Context(), req.Username, req.Email, req.Password, req.Role)
	if err != nil {
		httputil.Error(w, http.StatusConflict, "user already exists")
		return
	}
	httputil.JSON(w, http.StatusCreated, map[string]any{"id": id, "username": req.Username, "email": req.Email, "role": req.Role})
}

func (s *Server) validateClient(ctx context.Context, clientID, redirectURI string) error {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `
SELECT EXISTS(SELECT 1 FROM oauth_clients WHERE client_id = $1 AND redirect_uri = $2)
`, clientID, redirectURI).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Server) validateClientSecret(ctx context.Context, clientID, clientSecret string) error {
	var storedSecret string
	if err := s.db.QueryRowContext(ctx, `
SELECT client_secret
FROM oauth_clients
WHERE client_id = $1
`, clientID).Scan(&storedSecret); err != nil {
		return err
	}
	if storedSecret != "" && storedSecret != clientSecret {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Server) userByUsername(ctx context.Context, username string) (user, error) {
	var u user
	err := s.db.QueryRowContext(ctx, `
SELECT id, username, email, password_hash, role
FROM users
WHERE username = $1
`, username).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role)
	return u, err
}

func (s *Server) insertUser(ctx context.Context, username, email, password, role string) (int64, error) {
	hash, err := hashPassword(password)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `
INSERT INTO users(username, email, password_hash, role)
VALUES ($1, $2, $3, $4)
RETURNING id
`, username, email, hash, role).Scan(&id)
	return id, err
}

func (s *Server) signJWT(u user, audience, scope string) (string, error) {
	now := time.Now()
	payload := map[string]any{
		"iss":                s.cfg.Issuer,
		"sub":                u.Username,
		"aud":                audience,
		"iat":                now.Unix(),
		"exp":                now.Add(s.cfg.AccessTokenTTL).Unix(),
		"preferred_username": u.Username,
		"email":              u.Email,
		"roles":              []string{u.Role},
		"scope":              normalizeScope(scope),
	}
	header := map[string]string{"alg": "RS256", "typ": "JWT", "kid": keyID}
	head, err := encodeJSON(header)
	if err != nil {
		return "", err
	}
	body, err := encodeJSON(payload)
	if err != nil {
		return "", err
	}
	signingInput := head + "." + body
	sum := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, s.private, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func encodeJSON(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func normalizeScope(scope string) string {
	seen := map[string]bool{"openid": true}
	ordered := []string{"openid"}
	for _, item := range strings.Fields(scope) {
		if !seen[item] {
			seen[item] = true
			ordered = append(ordered, item)
		}
	}
	return strings.Join(ordered, " ")
}

func hashPassword(password string) (string, error) {
	salt, err := randomToken(16)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(salt + ":" + password))
	return salt + "$" + base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func checkPassword(stored, password string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 2 {
		return false
	}
	sum := sha256.Sum256([]byte(parts[0] + ":" + password))
	return parts[1] == base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Server) renderAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	page := strings.NewReplacer(
		"{{MESSAGE}}", html.EscapeString(message),
	).Replace(authErrorPage)
	_, _ = w.Write([]byte(page))
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

const loginPage = `<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>RSOI Identity</title>
  <style>
    *{box-sizing:border-box}
    body{margin:0;font-family:Inter,Arial,sans-serif;background:#eef2f7;color:#182230;min-height:100vh;display:grid;place-items:center;padding:24px}
    .screen{width:min(920px,100%);display:grid;grid-template-columns:1fr 420px;background:#fff;border:1px solid #dbe2ed;border-radius:8px;overflow:hidden;box-shadow:0 24px 80px rgba(15,23,42,.14)}
    .panel{background:linear-gradient(135deg,#141b2d,#164e63);color:white;padding:34px;display:flex;flex-direction:column;justify-content:space-between;min-height:520px}
    .brand{display:flex;gap:12px;align-items:center;font-weight:800;font-size:20px}
    .mark{width:42px;height:42px;display:grid;place-items:center;border-radius:8px;background:#2f6fed}
    .panel h1{font-size:34px;line-height:1.12;margin:0 0 12px}
    .panel p{color:#d6e3f3;margin:0;line-height:1.55}
    main{padding:34px;display:grid;align-content:center}
    main h2{font-size:26px;margin:0}
    .hint{margin:8px 0 24px;color:#667085}
    .form-title{display:none}
    .form-title.active{display:block}
    label{display:block;font-size:13px;color:#4d5663;margin:14px 0 6px;font-weight:700}
    input{width:100%;border:1px solid #c8ced8;border-radius:7px;padding:12px;font-size:15px;color:#182230}
    input:focus{outline:2px solid rgba(47,111,237,.22);border-color:#2f6fed}
    button{width:100%;margin-top:22px;border:0;border-radius:7px;background:#2f6fed;color:white;font-size:15px;font-weight:800;padding:12px;cursor:pointer}
    button:hover{background:#255ecb}
    .secondary{background:white;color:#2f6fed;border:1px solid #b8c9fb;margin-top:12px}
    .secondary:hover{background:#f4f7ff}
    .auth-form{display:none}
    .auth-form.active{display:block}
    @media(max-width:760px){.screen{grid-template-columns:1fr}.panel{min-height:auto}.panel h1{font-size:28px}}
  </style>
</head>
<body>
<section class="screen">
  <div class="panel">
    <div class="brand"><span class="mark">HB</span><span>Hotels Booking</span></div>
    <div>
      <h1>Единый вход для бронирований</h1>
      <p>После авторизации портал вернет вас к каталогу, бронированиям и профилю лояльности.</p>
    </div>
  </div>
  <main>
    <div id="login-title" class="form-title active">
      <h2>Вход</h2>
      <p class="hint">Введите учетные данные для продолжения.</p>
    </div>
    <div id="register-title" class="form-title">
      <h2>Регистрация</h2>
      <p class="hint">Создайте аккаунт, и портал сразу откроется под новым пользователем.</p>
    </div>
    <form id="login-form" class="auth-form active" method="post" action="/api/v1/login">
      <input type="hidden" name="client_id" value="{{CLIENT_ID}}">
      <input type="hidden" name="redirect_uri" value="{{REDIRECT_URI}}">
      <input type="hidden" name="scope" value="{{SCOPE}}">
      <input type="hidden" name="state" value="{{STATE}}">
      <label for="username">Логин</label>
      <input id="username" name="username" autocomplete="username" required>
      <label for="password">Пароль</label>
      <input id="password" name="password" type="password" autocomplete="current-password" required>
      <button type="submit">Войти</button>
    </form>
    <form id="register-form" class="auth-form" method="post" action="/api/v1/register">
      <input type="hidden" name="client_id" value="{{CLIENT_ID}}">
      <input type="hidden" name="redirect_uri" value="{{REDIRECT_URI}}">
      <input type="hidden" name="scope" value="{{SCOPE}}">
      <input type="hidden" name="state" value="{{STATE}}">
      <label for="reg-username">Логин</label>
      <input id="reg-username" name="username" autocomplete="username" required>
      <label for="reg-email">Email</label>
      <input id="reg-email" name="email" type="email" autocomplete="email" required>
      <label for="reg-password">Пароль</label>
      <input id="reg-password" name="password" type="password" autocomplete="new-password" required>
      <button type="submit">Зарегистрироваться</button>
    </form>
    <button id="mode-toggle" class="secondary" type="button">Зарегистрироваться</button>
  </main>
</section>
<script>
const toggle = document.getElementById('mode-toggle');
const loginForm = document.getElementById('login-form');
const registerForm = document.getElementById('register-form');
const loginTitle = document.getElementById('login-title');
const registerTitle = document.getElementById('register-title');
let registerMode = false;
toggle.addEventListener('click', () => {
  registerMode = !registerMode;
  loginForm.classList.toggle('active', !registerMode);
  registerForm.classList.toggle('active', registerMode);
  loginTitle.classList.toggle('active', !registerMode);
  registerTitle.classList.toggle('active', registerMode);
  toggle.textContent = registerMode ? 'У меня уже есть аккаунт' : 'Зарегистрироваться';
});
</script>
</body>
</html>`

const authErrorPage = `<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>RSOI Identity</title>
  <style>
    *{box-sizing:border-box}
    body{margin:0;font-family:Inter,Arial,sans-serif;background:#eef2f7;color:#182230;min-height:100vh;display:grid;place-items:center;padding:24px}
    .card{width:min(460px,100%);background:#fff;border:1px solid #dbe2ed;border-radius:8px;padding:28px;box-shadow:0 20px 60px rgba(15,23,42,.12)}
    h1{margin:0 0 8px;font-size:24px}
    p{margin:0;color:#5f6b7a;line-height:1.55}
    button{width:100%;margin-top:22px;border:0;border-radius:7px;background:#2f6fed;color:white;font-size:15px;font-weight:800;padding:12px;cursor:pointer}
  </style>
</head>
<body>
  <div class="card">
    <h1>Не удалось зарегистрироваться</h1>
    <p>{{MESSAGE}}</p>
    <button type="button" onclick="history.back()">Вернуться</button>
  </div>
</body>
</html>`
