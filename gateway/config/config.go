package config

import (
	"net"
	"strings"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Host string `env:"HOST" envDefault:"0.0.0.0"`
	Port string `env:"PORT" envDefault:"8080"`

	ReservationURL    string `env:"RESERVATION_URL,required"`
	PaymentURL        string `env:"PAYMENT_URL,required"`
	LoyaltyURL        string `env:"LOYALTY_URL,required"`
	IdentityURL       string `env:"IDENTITY_URL,required"`
	IdentityPublicURL string `env:"IDENTITY_PUBLIC_URL" envDefault:"http://localhost:8084"`
	StatisticsURL     string `env:"STATISTICS_URL,required"`

	JWTIssuer    string `env:"JWT_ISSUER" envDefault:"http://localhost:8084"`
	ClientID     string `env:"CLIENT_ID" envDefault:"rsoi-spa"`
	ClientSecret string `env:"CLIENT_SECRET" envDefault:""`
	RedirectURI  string `env:"REDIRECT_URI" envDefault:"http://localhost:8080/api/v1/callback"`
	UIURL        string `env:"UI_URL" envDefault:"http://localhost:3000"`
	KafkaBrokers string `env:"KAFKA_BROKERS" envDefault:""`
	KafkaTopic   string `env:"KAFKA_TOPIC" envDefault:"rsoi.events"`

	KafkaReservationCanceledTopic string `env:"KAFKA_RESERVATION_CANCELED_TOPIC" envDefault:"reservation.canceled"`
	KafkaReservationCreatedTopic  string `env:"KAFKA_RESERVATION_CREATED_TOPIC" envDefault:"reservation.created"`
	KafkaPaymentCancelTopic       string `env:"KAFKA_PAYMENT_CANCEL_TOPIC" envDefault:"payment.cancel.requested"`
}

func Load() (Config, error) {
	cfg := Config{}

	if err := env.Parse(&cfg); err != nil {
		return Config{}, err
	}
	cfg.ReservationURL = strings.TrimRight(cfg.ReservationURL, "/")
	cfg.PaymentURL = strings.TrimRight(cfg.PaymentURL, "/")
	cfg.LoyaltyURL = strings.TrimRight(cfg.LoyaltyURL, "/")
	cfg.IdentityURL = strings.TrimRight(cfg.IdentityURL, "/")
	cfg.IdentityPublicURL = strings.TrimRight(cfg.IdentityPublicURL, "/")
	cfg.StatisticsURL = strings.TrimRight(cfg.StatisticsURL, "/")
	cfg.UIURL = strings.TrimRight(cfg.UIURL, "/")

	return cfg, nil
}

func (c Config) Address() string {
	return net.JoinHostPort(c.Host, c.Port)
}
