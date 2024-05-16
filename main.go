package main

import (
	"bytes"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"log"
	"log/slog"
	"os"
	"regexp"
	"time"

	"github.com/disintegration/imaging"
	"github.com/pkg/errors"
	"github.com/wagslane/go-rabbitmq"
)

const thumbnailWidth = 200
const thumbnailHeight = 200

func main() {
	if err := mainInner(); err != nil {
		slog.Error("exit with error", "error", err)
		os.Exit(1)
	}
}

func publishReply(publisher *rabbitmq.Publisher, logger *slog.Logger, payload []byte, d *rabbitmq.Delivery) bool {
	if d.ReplyTo != "" {
		if err := publisher.Publish(
			payload,
			[]string{d.ReplyTo},
			rabbitmq.WithPublishOptionsCorrelationID(d.MessageId),
		); err != nil {
			logger.Error("failed to publish reply message", "error", err)
			return false
		}
		logger.Info("reply message published")
	} else {
		logger.Warn("skipped reply message due to missing Reply-To field")
	}
	return true
}

func mainInner() error {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{})))

	connectionString := os.Getenv("AMQP_CONNECTION")
	if connectionString == "" {
		return errors.New("empty 'AMQP_CONNECTION'")
	}

	thumbnailGenerationRoutingKey := os.Getenv("AMQP_THUMBNAILING_ROUTING_KEY")
	if thumbnailGenerationRoutingKey == "" {
		return errors.New("empty 'AMQP_THUMBNAILING_ROUTING_KEY'")
	}

	slog.Info("connecting to AMQP", "conn", regexp.MustCompile("://.+@").ReplaceAllString(connectionString, "://<masked>@"))
	conn, err := rabbitmq.NewConn(
		connectionString,
		rabbitmq.WithConnectionOptionsLogging,
	)
	if err != nil {
		return errors.Wrap(err, "failed to connect to AMQP")
	}
	defer func() {
		slog.Info("closing AMQP")
		if err := conn.Close(); err != nil {
			slog.Error("error while closing AMQP", "error", err)
		}
	}()

	publisher, err := rabbitmq.NewPublisher(
		conn,
		rabbitmq.WithPublisherOptionsLogging,
	)
	if err != nil {
		return errors.Wrap(err, "failed to create publisher")
	}
	defer publisher.Close()

	slog.Info("binding consumer to default exchange", "queue", thumbnailGenerationRoutingKey, "routing-key", thumbnailGenerationRoutingKey)
	consumer, err := rabbitmq.NewConsumer(
		conn,
		thumbnailGenerationRoutingKey,
	)
	if err != nil {
		log.Fatal(err)
	}
	defer consumer.Close()

	if err = consumer.Run(func(d rabbitmq.Delivery) (action rabbitmq.Action) {
		start := time.Now()
		subLogger := slog.Default().With("delivery_tag", d.DeliveryTag, "message_id", d.MessageId, "input_bytes", len(d.Body))
		subLogger.Info("decoding image")
		img, format, err := image.Decode(bytes.NewReader(d.Body))
		if err != nil {
			subLogger.Warn("failed to decode image", "error", err)
			if !publishReply(publisher, subLogger, []byte("DecodeFailed"), &d) {
				// requeue to ensure we send the failure message
				return rabbitmq.NackRequeue
			}
			// discard since the body could not be decoded
			return rabbitmq.NackDiscard
		}
		subLogger = subLogger.With("format", format, "input_size", img.Bounds().Size().String())
		thumbnail := imaging.Thumbnail(img, thumbnailWidth, thumbnailHeight, imaging.Lanczos)
		subLogger = subLogger.With("output_size", thumbnail.Bounds().Size().String())
		subLogger.Info("generated thumbnail", slog.Duration("elapsed", time.Since(start)))
		buff := new(bytes.Buffer)
		if err := jpeg.Encode(buff, thumbnail, nil); err != nil {
			subLogger.Warn("failed to encode thumbnail", "error", err)
			if !publishReply(publisher, subLogger, []byte("EncodeFailed"), &d) {
				// requeue to ensure we send the failure message
				return rabbitmq.NackRequeue
			}
			// discard since the body could not be encoded
			return rabbitmq.NackDiscard
		}
		ratio := float64(len(buff.Bytes())) / float64(len(d.Body))
		subLogger = subLogger.With("output_bytes", len(buff.Bytes()), "ratio", ratio)
		subLogger.Info("encoded thumbnail", slog.Duration("elapsed", time.Since(start)))

		if !publishReply(publisher, subLogger, buff.Bytes(), &d) {
			// requeue to ensure we send the success message
			return rabbitmq.NackRequeue
		}

		return rabbitmq.Ack
	}); err != nil {
		return errors.Wrap(err, "failure during consumption")
	}
	return nil
}
