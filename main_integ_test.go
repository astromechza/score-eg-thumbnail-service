package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/wagslane/go-rabbitmq"
)

func TestMainIntegration(t *testing.T) {
	connectionString := os.Getenv("AMQP_CONNECTION")
	if connectionString == "" {
		t.Skip("empty 'AMQP_CONNECTION'")
		return
	}

	thumbnailGenerationRoutingKey := os.Getenv("AMQP_THUMBNAILING_ROUTING_KEY")
	if thumbnailGenerationRoutingKey == "" {
		t.Skip("empty 'AMQP_THUMBNAILING_ROUTING_KEY'")
		return
	}

	rawLenna, err := os.ReadFile("samples/library.jpg")
	if err != nil {
		t.Fatal(err)
	}

	t.Log("connecting to rabbitmq")
	conn, err := rabbitmq.NewConn(
		connectionString,
		rabbitmq.WithConnectionOptionsLogging,
	)
	if err != nil {
		t.Fatal("failed to connect", err)
	}
	defer conn.Close()

	publisher, err := rabbitmq.NewPublisher(
		conn,
		rabbitmq.WithPublisherOptionsLogging,
	)
	if err != nil {
		t.Fatal("failed to create publisher", err)
	}
	defer publisher.Close()

	rcvQueueName := fmt.Sprintf("receive-%d", int(rand.Int64()))
	consumer, err := rabbitmq.NewConsumer(
		conn,
		rcvQueueName,
		rabbitmq.WithConsumerOptionsConsumerAutoAck(true),
		rabbitmq.WithConsumerOptionsQueueAutoDelete,
	)
	if err != nil {
		t.Fatal("failed to create consumer", err)
	}
	defer consumer.Close()

	messageId := strconv.Itoa(int(rand.Int64()))
	if err := publisher.Publish(
		rawLenna,
		[]string{thumbnailGenerationRoutingKey},
		rabbitmq.WithPublishOptionsMessageID(messageId),
		rabbitmq.WithPublishOptionsReplyTo(rcvQueueName),
	); err != nil {
		t.Fatal("failed to publish message", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	go func() {
		<-ctx.Done()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			consumer.Close()
		}
	}()

	locker := new(sync.Mutex)
	deliveries := make([]rabbitmq.Delivery, 0, 1)

	if err := consumer.Run(func(d rabbitmq.Delivery) (action rabbitmq.Action) {
		locker.Lock()
		defer locker.Unlock()
		deliveries = append(deliveries, d)
		consumer.Close()
		return rabbitmq.Ack
	}); err != nil {
		t.Fatal(err)
	}

	locker.Lock()
	defer locker.Unlock()

	t.Log("output", deliveries)

	if len(deliveries) != 1 {
		t.Fatal("expected 1 delivery")
	}
	if len(deliveries[0].Body) < 100 {
		t.Log("response", string(deliveries[0].Body))
		t.Fatal("expected a successful response")
	}
	_ = os.WriteFile("samples/library_output.jpeg", deliveries[0].Body, 0644)
}
