package kafka

import (
	"context"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
)

// ProduceMessage sends a message to the given Kafka topic.
func ProduceMessage(brokerAddress, topic, message string) error {
	writer := kafka.NewWriter(kafka.WriterConfig{
		Brokers:  []string{brokerAddress},
		Topic:    topic,
		Balancer: &kafka.LeastBytes{},
	})
	defer writer.Close()

	// Create a context with a 10-second timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	msg := kafka.Message{
		Key:   []byte(fmt.Sprintf("Key-%d", time.Now().Unix())),
		Value: []byte(message),
	}

	return writer.WriteMessages(ctx, msg)
}