package config

import (
	"net"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Host string `env:"HOST" envDefault:"0.0.0.0"`
	Port string `env:"PORT" envDefault:"8084"`

	DatabaseURL string `env:"DB_URL,required"`

	Issuer          string        `env:"ISSUER" envDefault:"http://localhost:8084"`
	AccessTokenTTL  time.Duration `env:"ACCESS_TOKEN_TTL" envDefault:"1h"`
	AuthCodeTTL     time.Duration `env:"AUTH_CODE_TTL" envDefault:"5m"`
	DefaultClientID string        `env:"DEFAULT_CLIENT_ID" envDefault:"rsoi-spa"`
	DefaultRedirect string        `env:"DEFAULT_REDIRECT_URI" envDefault:"http://localhost:8080/api/v1/callback"`

	AdminUsername string `env:"ADMIN_USERNAME" envDefault:"admin"`
	AdminPassword string `env:"ADMIN_PASSWORD" envDefault:"admin"`
	AdminEmail    string `env:"ADMIN_EMAIL" envDefault:"admin@example.com"`

	DefaultUserUsername string `env:"DEFAULT_USER_USERNAME" envDefault:"Test Max"`
	DefaultUserPassword string `env:"DEFAULT_USER_PASSWORD" envDefault:"test"`
	DefaultUserEmail    string `env:"DEFAULT_USER_EMAIL" envDefault:"test.max@example.com"`
}

func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Address() string {
	return net.JoinHostPort(c.Host, c.Port)
}
