package config

import (
	"net"
	"strings"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Host string `env:"HOST" envDefault:"0.0.0.0"`
	Port string `env:"PORT" envDefault:"8050"`

	DatabaseURL string `env:"DB_URL,required"`
	IdentityURL string `env:"IDENTITY_URL,required"`
	JWTIssuer   string `env:"JWT_ISSUER" envDefault:"http://localhost:8084"`
}

func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, err
	}
	cfg.IdentityURL = strings.TrimRight(cfg.IdentityURL, "/")
	return cfg, nil
}

func (c Config) Address() string {
	return net.JoinHostPort(c.Host, c.Port)
}
