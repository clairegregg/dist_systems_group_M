package kafka

import (
	"log"
	"time"

	kafkaGo "github.com/segmentio/kafka-go"
)

// CreateTopicIfNotExists creates a Kafka topic if it doesn't already exist.
func CreateTopicIfNotExists(brokerAddress, topic string, numPartitions, replicationFactor int) error {
	// Dial a connection to the broker.
	conn, err := kafkaGo.Dial("tcp", brokerAddress)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Set a deadline for the operation.
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// Create the topic with the provided configuration.
	err = conn.CreateTopics(kafkaGo.TopicConfig{
		Topic:             topic,
		NumPartitions:     numPartitions,
		ReplicationFactor: replicationFactor,
	})
	if err != nil {
		log.Printf("Error creating topic %s: %v", topic, err)
		return err
	}

	log.Printf("Topic %s created successfully", topic)
	return nil
}