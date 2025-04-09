package kafka

import (
	"fmt"
	"log"

	"github.com/IBM/sarama"
)

// Topics that will be used in the communication.
const (
	TopicCentralToChunk = "central_to_chunk"
	TopicChunkToCentral = "chunk_to_central"
)

// Kafka Producer struct
type Producer struct {
	producer sarama.SyncProducer
}

// NewProducer initializes a Kafka producer
func NewProducer(brokers []string) (*Producer, error) {
	config := sarama.NewConfig()
	config.Producer.Return.Successes = true

	producer, err := sarama.NewSyncProducer(brokers, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
	}

	return &Producer{producer: producer}, nil
}

// SendMessage sends a message to the specified topic.
func (p *Producer) SendMessage(topic, value string) error {
	msg := &sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.StringEncoder(value),
	}

	_, _, err := p.producer.SendMessage(msg)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	// log.Printf("Message sent to topic %s on partition %d with offset %d", topic, partition, offset)
	return nil
}

// Close shuts down the producer
func (p *Producer) Close() {
	if err := p.producer.Close(); err != nil {
		log.Printf("Error closing Kafka producer: %v", err)
	}
}

// Kafka Consumer struct
type Consumer struct {
	consumer sarama.Consumer
}

// NewConsumer initializes a Kafka consumer
func NewConsumer(brokers []string, group string) (*Consumer, error) {
	config := sarama.NewConfig()
	consumer, err := sarama.NewConsumer(brokers, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka consumer: %w", err)
	}
	return &Consumer{consumer: consumer}, nil
}

// ConsumeMessage consumes messages from the specified topic.
func (c *Consumer) ConsumeMessage(topic string) (string, error) {
	partitionConsumer, err := c.consumer.ConsumePartition(topic, 0, sarama.OffsetNewest)
	if err != nil {
		return "", fmt.Errorf("error starting partition consumer: %w", err)
	}
	defer partitionConsumer.Close()

	select {
	case msg := <-partitionConsumer.Messages():
		return string(msg.Value), nil
	}
}

// Close shuts down the consumer
func (c *Consumer) Close() {
	if err := c.consumer.Close(); err != nil {
		log.Printf("Error closing Kafka consumer: %v", err)
	}
}