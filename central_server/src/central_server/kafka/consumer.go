package kafka

import (
	"context"
	"log"

	kafkaGo "github.com/segmentio/kafka-go"
)

// StartConsumer starts consuming messages from the given topic.
func StartConsumer(brokerAddress, topic, groupID string) {
	reader := kafkaGo.NewReader(kafkaGo.ReaderConfig{
		Brokers: []string{brokerAddress},
		Topic:   topic,
		GroupID: groupID,
		MinBytes: 10e3,
		MaxBytes: 10e6,
	})
	defer reader.Close()

	for {
		msg, err := reader.ReadMessage(context.Background())
		if err != nil {
			log.Printf("Error reading message: %v", err)
			continue
		}
		log.Printf("Received message: key=%s, value=%s", string(msg.Key), string(msg.Value))
	}
}