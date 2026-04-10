package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

// ConnectNATS connects to a NATS server with the reconnect policy and logging
// handlers used by every Lotus Exchange service:
//
//   - Infinite reconnect attempts (MaxReconnects=-1) with a 2s backoff.
//   - 30s drain timeout so in-flight messages finish on shutdown.
//   - Disconnect, reconnect and error callbacks wired to the supplied logger.
//
// If url is empty the nats default (nats://127.0.0.1:4222) is used. The context
// is currently only used to keep the signature consistent with the other
// service helpers; nats.Connect does not accept one directly.
func ConnectNATS(_ context.Context, url string, name string, log *slog.Logger) (*nats.Conn, error) {
	if url == "" {
		url = nats.DefaultURL
	}

	nc, err := nats.Connect(url,
		nats.Name(name),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.DrainTimeout(30*time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil && log != nil {
				log.Warn("nats disconnected", "service", name, "error", err)
			}
		}),
		nats.ReconnectHandler(func(c *nats.Conn) {
			if log != nil {
				log.Info("nats reconnected", "service", name, "url", c.ConnectedUrl())
			}
		}),
		nats.ErrorHandler(func(_ *nats.Conn, sub *nats.Subscription, err error) {
			if log == nil {
				return
			}
			if sub != nil {
				log.Error("nats async error", "service", name, "subject", sub.Subject, "error", err)
			} else {
				log.Error("nats async error", "service", name, "error", err)
			}
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("service: nats connect %q: %w", url, err)
	}
	return nc, nil
}
