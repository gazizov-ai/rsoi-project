package config

import (
	"net"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Host string `env:"HOST" envDefault:"0.0.0.0"`
	Port string `env:"PORT" envDefault:"8080"`

	ReservationURL string `env:"RESERVATION_URL,required"`
	PaymentURL     string `env:"PAYMENT_URL,required"`
	LoyaltyURL     string `env:"LOYALTY_URL,required"`
	IdentityURL    string `env:"IDENTITY_URL,required"`
	StatisticsURL  string `env:"STATISTICS_URL,required"`

	JWTSecret string `env:"JWT_SECRET,required"`
}

func Load() (Config, error) {
	cfg := Config{}

	if err := env.Parse(&cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Address() string {
	return net.JoinHostPort(c.Host, c.Port)
}
