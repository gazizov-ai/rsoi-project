package events

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

type Event struct {
	Type      string         `json:"type"`
	Username  string         `json:"username"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"createdAt"`
}

type Publisher struct {
	writer *kafka.Writer
}

func NewPublisher(brokersCSV, topic string) *Publisher {
	brokers := splitCSV(brokersCSV)
	if len(brokers) == 0 {
		return nil
	}
	return &Publisher{
		writer: &kafka.Writer{
			Addr:         kafka.TCP(brokers...),
			Topic:        topic,
			Balancer:     &kafka.LeastBytes{},
			RequiredAcks: kafka.RequireOne,
		},
	}
}

func (p *Publisher) Publish(ctx context.Context, event Event) error {
	if p == nil || p.writer == nil {
		return nil
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return p.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(event.Username),
		Value: data,
		Time:  event.CreatedAt,
	})
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
