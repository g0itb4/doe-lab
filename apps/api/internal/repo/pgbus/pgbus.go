// Package pgbus carries the envelope signal between API instances with
// PostgreSQL LISTEN and NOTIFY.
package pgbus

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"doelab/api/internal/repo/bus"
	"doelab/api/internal/service"
)

// channel is the NOTIFY channel that carries envelope signals.
const channel = "doelab_envelopes"

// maxPayloadIDs is how many site ids go in one notification. A payload may be
// 8000 bytes; a UUID and its comma are 37.
const maxPayloadIDs = 200

// Bus carries the signal between API instances with LISTEN and NOTIFY.
//
// Notify sends the site ids to the database; every instance, this one
// included, hears them on its listening connection and signals its own
// subscribers through a bus.Local. There is one path, so an instance treats its
// own publishes and its neighbours' alike.
//
// A notification is lost if no connection is listening when it is sent: while
// an instance reconnects, say. That costs a subscriber time, not correctness:
// it reads the current envelope again at its next keepalive.
type Bus struct {
	*bus.Local
	pool *pgxpool.Pool
	log  *slog.Logger
	// ready is closed when the first LISTEN is in place.
	ready chan struct{}
}

// New returns a bus on the pool. Call Run to start listening.
func New(pool *pgxpool.Pool, log *slog.Logger) *Bus {
	return &Bus{Local: bus.NewLocal(), pool: pool, log: log, ready: make(chan struct{})}
}

var _ service.EnvelopeBus = (*Bus)(nil)

// Notify sends the site ids to every listening instance.
func (b *Bus) Notify(ctx context.Context, siteIDs []uuid.UUID) error {
	for start := 0; start < len(siteIDs); start += maxPayloadIDs {
		batch := siteIDs[start:min(start+maxPayloadIDs, len(siteIDs))]
		ids := make([]string, len(batch))
		for i, id := range batch {
			ids[i] = id.String()
		}
		if _, err := b.pool.Exec(ctx, `SELECT pg_notify($1, $2)`, channel, strings.Join(ids, ",")); err != nil {
			return err
		}
	}
	return nil
}

// Ready is closed once the bus is listening.
func (b *Bus) Ready() <-chan struct{} { return b.ready }

// Run listens until ctx ends, reconnecting when the connection is lost. It
// returns nil when ctx ends.
func (b *Bus) Run(ctx context.Context) error {
	backoff := 100 * time.Millisecond
	first := true
	for {
		err := b.listen(ctx, func() {
			if first {
				close(b.ready)
				first = false
			}
			backoff = 100 * time.Millisecond
		})
		if ctx.Err() != nil {
			return nil
		}
		b.log.WarnContext(ctx, "envelope bus lost its connection; reconnecting", "err", err, "in", backoff.String())
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 5*time.Second)
	}
}

// listen holds one connection and delivers notifications until it fails.
func (b *Bus) listen(ctx context.Context, listening func()) error {
	conn, err := b.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	// The connection has LISTEN state on it; do not hand it back to the pool
	// for another caller to inherit.
	defer func() {
		raw := conn.Hijack()
		_ = raw.Close(context.WithoutCancel(ctx))
	}()

	if _, err := conn.Exec(ctx, "LISTEN "+channel); err != nil {
		return err
	}
	listening()
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var ids []uuid.UUID
		for _, raw := range strings.Split(n.Payload, ",") {
			if id, err := uuid.Parse(raw); err == nil {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			err = errors.New("a notification with no site id")
			b.log.WarnContext(ctx, "envelope bus: ignored a notification", "payload", n.Payload, "err", err)
			continue
		}
		_ = b.Local.Notify(ctx, ids)
	}
}
