package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

// Producer is a generic Kafka producer.
type Producer struct {
	writer *kafkago.Writer
}

// NewProducer creates a new Kafka producer.
func NewProducer(brokers []string) *Producer {
	return &Producer{
		writer: &kafkago.Writer{
			Addr:         kafkago.TCP(brokers...),
			Balancer:     &kafkago.LeastBytes{},
			MaxAttempts:  3,
			BatchSize:    100,
			BatchTimeout: 10 * time.Millisecond,
			WriteTimeout: 10 * time.Second,
			ReadTimeout:  10 * time.Second,
		},
	}
}

// Produce sends a message to the specified topic.
func (p *Producer) Produce(
	ctx context.Context,
	topic string,
	key string,
	payload interface{},
) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal kafka message: %w", err)
	}

	msg := kafkago.Message{
		Topic: topic,
		Key:   []byte(key),
		Value: data,
	}

	if err := p.writer.WriteMessages(ctx, msg); err != nil {
		return fmt.Errorf("failed to write kafka message to %s: %w", topic, err)
	}
	return nil
}

// Close closes the producer.
func (p *Producer) Close() error {
	return p.writer.Close()
}

// Consumer is a generic Kafka consumer.
type Consumer struct {
	reader      *kafkago.Reader
	concurrency int
}

// ConsumerOption configures a Consumer.
type ConsumerOption func(*Consumer)

// WithConcurrency sets the number of concurrent message handlers.
func WithConcurrency(n int) ConsumerOption {
	return func(c *Consumer) {
		if n < 1 {
			n = 1
		}
		c.concurrency = n
	}
}

// NewConsumer creates a new Kafka consumer.
func NewConsumer(brokers []string, topic, groupID string, opts ...ConsumerOption) *Consumer {
	c := &Consumer{
		reader: kafkago.NewReader(kafkago.ReaderConfig{
			Brokers:        brokers,
			Topic:          topic,
			GroupID:        groupID,
			MinBytes:       10e3,
			MaxBytes:       10e6,
			CommitInterval: time.Second,
		}),
		concurrency: 1,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Message represents a consumed Kafka message.
type Message struct {
	Topic  string
	Key    string
	Value  []byte
	Offset int64
}

// Handler is a function that processes a Kafka message.
type Handler func(ctx context.Context, msg Message) error

// Consume starts consuming messages and passes them to the handler.
// A pool of concurrency workers processes messages in parallel.
func (c *Consumer) Consume(ctx context.Context, handler Handler) error {
	jobs := make(chan Message, c.concurrency*2)

	var wg sync.WaitGroup
	for i := 0; i < c.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for msg := range jobs {
				if err := handler(ctx, msg); err != nil {
					fmt.Printf("handler error for topic %s: %v\n", msg.Topic, err)
				}
			}
		}()
	}

	for {
		m, err := c.reader.ReadMessage(ctx)
		if err != nil {
			close(jobs)
			wg.Wait()
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("failed to read message: %w", err)
		}

		select {
		case jobs <- Message{
			Topic:  m.Topic,
			Key:    string(m.Key),
			Value:  m.Value,
			Offset: m.Offset,
		}:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return nil
		}
	}
}

// Close closes the consumer.
func (c *Consumer) Close() error {
	return c.reader.Close()
}

// EnsureTopics creates topics if they do not exist (best-effort for dev).
func EnsureTopics(brokers []string, topics []string, partitions, replicas int) error {
	conn, err := kafkago.Dial("tcp", brokers[0])
	if err != nil {
		return fmt.Errorf("failed to dial kafka: %w", err)
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("failed to get controller: %w", err)
	}

	controllerConn, err := kafkago.Dial(
		"tcp",
		fmt.Sprintf("%s:%d", controller.Host, controller.Port),
	)
	if err != nil {
		return fmt.Errorf("failed to dial controller: %w", err)
	}
	defer controllerConn.Close()

	for _, topic := range topics {
		topicConfig := kafkago.TopicConfig{
			Topic:             topic,
			NumPartitions:     partitions,
			ReplicationFactor: replicas,
		}
		if err := controllerConn.CreateTopics(topicConfig); err != nil {
			fmt.Printf("topic %s may already exist: %v\n", topic, err)
		}
	}

	return nil
}
