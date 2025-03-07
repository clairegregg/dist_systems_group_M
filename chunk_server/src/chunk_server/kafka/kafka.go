package kafka

import (
	"fmt"
	"log"

	"github.com/IBM/sarama"
)

// Producer struct for Kafka
type Producer struct {
	producer sarama.SyncProducer
}

// NewProducer initializes a Kafka producer
func NewProducer(broker string) (*Producer, error) {
	config := sarama.NewConfig()
	config.Producer.Return.Successes = true

	producer, err := sarama.NewSyncProducer([]string{broker}, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
	}

	return &Producer{producer: producer}, nil
}

// SendMessage sends a message to a Kafka topic
func (p *Producer) SendMessage(topic, value string) error {
	msg := &sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.StringEncoder(value),
	}

	partition, offset, err := p.producer.SendMessage(msg)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	log.Printf("Message sent to topic %s on partition %d with offset %d", topic, partition, offset)
	return nil
}

// CreateTopic creates a Kafka topic if it does not exist
func (p *Producer) CreateTopic(topic string) error {
	config := sarama.NewConfig()
	config.Version = sarama.V2_1_0_0

	admin, err := sarama.NewClusterAdmin([]string{"kafka:9092"}, config)
	if err != nil {
		return fmt.Errorf("error creating cluster admin: %v", err)
	}
	defer admin.Close()

	err = admin.CreateTopic(topic, &sarama.TopicDetail{
		NumPartitions:     1,
		ReplicationFactor: 1,
	}, false)
	if err != nil {
		log.Printf("Error creating topic %s: %v", topic, err)
	}
	return nil
}

// Close shuts down the producer
func (p *Producer) Close() {
	if err := p.producer.Close(); err != nil {
		log.Printf("Error closing Kafka producer: %v", err)
	}
}

// Consumer struct for Kafka
type Consumer struct {
	consumer sarama.Consumer
}

// NewConsumer initializes a Kafka consumer
func NewConsumer(broker string, group string) (*Consumer, error) {
	config := sarama.NewConfig()
	consumer, err := sarama.NewConsumer([]string{broker}, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka consumer: %w", err)
	}
	return &Consumer{consumer: consumer}, nil
}

// ConsumeMultiTopic listens to multiple topics and returns a message from any of them
func (c *Consumer) ConsumeMultiTopic(topics []string) (string, string, error) {
	for _, topic := range topics {
		partitionConsumer, err := c.consumer.ConsumePartition(topic, 0, sarama.OffsetNewest)
		if err != nil {
			log.Printf("Error consuming topic %s: %v", topic, err)
			continue
		}
		defer partitionConsumer.Close()

		select {
		case msg := <-partitionConsumer.Messages():
			return string(msg.Value), topic, nil
		}
	}
	return "", "", fmt.Errorf("no messages received")
}

func (c *Consumer) ConsumeMessage(topic string) (string, error) {
	// This example assumes you consume from partition 0.
	partitionConsumer, err := c.consumer.ConsumePartition(topic, 0, sarama.OffsetNewest)
	if err != nil {
		return "", err
	}
	defer partitionConsumer.Close()
	
	msg := <-partitionConsumer.Messages()
	return string(msg.Value), nil
}

// Close shuts down the consumer
func (c *Consumer) Close() {
	if err := c.consumer.Close(); err != nil {
		log.Printf("Error closing Kafka consumer: %v", err)
	}
}