module github.com/gazizov-ai/rsoi-project/reservation

go 1.23.0

require (
	github.com/caarlos0/env/v11 v11.4.0
	github.com/gazizov-ai/rsoi-project/common v0.0.0
	github.com/go-chi/chi/v5 v5.2.5
	github.com/google/uuid v1.6.0
	github.com/jmoiron/sqlx v1.4.0
	github.com/lib/pq v1.12.3
)

replace github.com/gazizov-ai/rsoi-project/common => ../common
