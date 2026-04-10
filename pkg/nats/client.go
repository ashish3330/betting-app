package nats

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type Client struct {
	conn   *nats.Conn
	js     jetstream.JetStream
	logger *slog.Logger
}

func NewClient(url string, logger *slog.Logger) (*Client, error) {
	opts := []nats.Option{
		nats.Name("lotus-exchange"),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(-1),
		// Cap Drain() so graceful shutdown cannot block forever when a
		// broker is unreachable or a slow subscriber is backed up.
		nats.DrainTimeout(30 * time.Second),
		nats.ReconnectHandler(func(c *nats.Conn) {
			logger.Info("NATS reconnected", "url", c.ConnectedUrl())
		}),
		nats.DisconnectErrHandler(func(c *nats.Conn, err error) {
			if err != nil {
				logger.Warn("NATS disconnected", "error", err)
			}
		}),
	}

	conn, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect to NATS: %w", err)
	}

	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("create JetStream: %w", err)
	}

	return &Client{conn: conn, js: js, logger: logger}, nil
}

func (c *Client) CreateStream(ctx context.Context, name string, subjects []string) (jetstream.Stream, error) {
	stream, err := c.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      name,
		Subjects:  subjects,
		Retention: jetstream.WorkQueuePolicy,
		MaxAge:    24 * time.Hour,
		Storage:   jetstream.FileStorage,
		Replicas:  1,
	})
	if err != nil {
		return nil, fmt.Errorf("create stream %s: %w", name, err)
	}
	c.logger.Info("stream created/updated", "name", name, "subjects", subjects)
	return stream, nil
}

func (c *Client) Publish(ctx context.Context, subject string, data []byte) error {
	_, err := c.js.Publish(ctx, subject, data)
	if err != nil {
		return fmt.Errorf("publish to %s: %w", subject, err)
	}
	return nil
}

func (c *Client) Subscribe(ctx context.Context, stream, consumer, filterSubject string, handler func(msg jetstream.Msg)) (jetstream.ConsumeContext, error) {
	cons, err := c.js.CreateOrUpdateConsumer(ctx, stream, jetstream.ConsumerConfig{
		Durable:       consumer,
		FilterSubject: filterSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    5,
		AckWait:       30 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("create consumer %s: %w", consumer, err)
	}

	cc, err := cons.Consume(func(msg jetstream.Msg) {
		handler(msg)
	})
	if err != nil {
		return nil, fmt.Errorf("consume %s: %w", consumer, err)
	}

	return cc, nil
}

func (c *Client) Conn() *nats.Conn { return c.conn }

func (c *Client) JetStream() jetstream.JetStream { return c.js }

func (c *Client) Close() {
	if c.conn != nil {
		// Drain flushes in-flight messages and then closes the connection,
		// so an explicit Close() afterwards is unnecessary.
		if err := c.conn.Drain(); err != nil {
			c.logger.Warn("NATS drain error", "error", err)
		}
	}
}
