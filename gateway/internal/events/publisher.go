package events

import (
	"context"
	"encoding/json"
	"errors"
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
	writer       *kafka.Writer
	defaultTopic string
}

func NewPublisher(brokersCSV, topic string) *Publisher {
	brokers := splitCSV(brokersCSV)
	if len(brokers) == 0 {
		return nil
	}
	return &Publisher{
		defaultTopic: topic,
		writer: &kafka.Writer{
			Addr:         kafka.TCP(brokers...),
			Balancer:     &kafka.LeastBytes{},
			RequiredAcks: kafka.RequireOne,
		},
	}
}

func (p *Publisher) Enabled() bool {
	return p != nil && p.writer != nil
}

func (p *Publisher) Publish(ctx context.Context, event Event) error {
	return p.PublishTo(ctx, p.defaultTopic, event)
}

func (p *Publisher) PublishTo(ctx context.Context, topic string, event Event) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	return p.PublishJSON(ctx, topic, event.Username, event)
}

func (p *Publisher) PublishJSON(ctx context.Context, topic, key string, value any) error {
	if p == nil || p.writer == nil {
		return nil
	}
	if strings.TrimSpace(topic) == "" {
		topic = p.defaultTopic
	}
	if strings.TrimSpace(topic) == "" {
		return errors.New("kafka topic is empty")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return p.writer.WriteMessages(ctx, kafka.Message{
		Topic: topic,
		Key:   []byte(key),
		Value: data,
		Time:  time.Now().UTC(),
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
