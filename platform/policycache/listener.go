package policycache

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// PostgresListener wraps a *dedicated* *pgx.Conn and surfaces Postgres
// LISTEN/NOTIFY events as callbacks.
//
// # Conn lifetime contract
//
// The conn passed to NewListener MUST be a dedicated connection (typically
// acquired via pgxpool.Pool.Acquire(ctx) and held for the lifetime of the
// listener). Do NOT pass a pgxpool — pool connections are recycled, and
// LISTEN subscriptions silently disappear when a connection is returned to
// the pool. A pgxpool.Conn obtained from Acquire is suitable; pass its
// .Conn() to NewListener.
//
// # Error model
//
//   - Listen returns nil on a clean context cancellation.
//   - Listen returns the underlying error on conn failure or LISTEN
//     statement failure. Before returning, it invokes onError(err) so the
//     caller can record metrics / schedule reconnect.
//   - The listener does NOT attempt to reconnect on its own. Reconnect is
//     a caller policy concern (the caller knows the pool, retry budget,
//     backoff strategy, etc.).
//
// # Callback discipline
//
//   - onNotify is invoked synchronously from the listen loop. It MUST
//     return quickly; blocking work belongs in a separate goroutine. A
//     slow onNotify starves the listen loop and may cause Postgres to
//     buffer notifications (eventually applying NOTIFY backpressure).
//   - onError is invoked once, just before Listen returns, when an
//     unrecoverable error occurs. It is NOT called on clean cancellation.
type PostgresListener struct {
	conn *pgx.Conn
}

// NewListener constructs a PostgresListener bound to the dedicated conn.
// Panics if conn is nil — passing a nil conn is always a programmer error.
func NewListener(conn *pgx.Conn) *PostgresListener {
	if conn == nil {
		panic("policycache: NewListener: conn must not be nil")
	}
	return &PostgresListener{conn: conn}
}

// Listen issues `LISTEN <channel>` then blocks, calling onNotify for each
// notification on that channel. Returns nil when ctx is cancelled; returns
// a non-nil error (and invokes onError once with the same error) when the
// connection fails or LISTEN cannot be issued.
//
// channel must be a valid Postgres identifier (it is interpolated into the
// LISTEN statement after a pgx.Identifier sanitization pass). An empty
// channel name returns an error without touching the connection.
//
// onNotify and onError must both be non-nil; pass no-op closures if you
// don't care about a particular signal.
func (l *PostgresListener) Listen(
	ctx context.Context,
	channel string,
	onNotify func(payload string),
	onError func(err error),
) error {
	if channel == "" {
		return errors.New("policycache: Listen: channel must not be empty")
	}
	if onNotify == nil || onError == nil {
		return errors.New("policycache: Listen: onNotify and onError must be non-nil")
	}

	// Sanitize the channel name. pgx.Identifier.Sanitize produces a
	// quoted identifier safe to interpolate into SQL.
	quoted := pgx.Identifier{channel}.Sanitize()
	stmt := fmt.Sprintf("LISTEN %s", quoted)

	if _, err := l.conn.Exec(ctx, stmt); err != nil {
		wrapped := fmt.Errorf("policycache: LISTEN %s: %w", channel, err)
		onError(wrapped)
		return wrapped
	}

	for {
		// WaitForNotification returns when:
		//   - a notification arrives (any channel on this conn)
		//   - the context is cancelled (returns ctx.Err)
		//   - the connection errors (returns the conn error)
		n, err := l.conn.WaitForNotification(ctx)
		if err != nil {
			// Clean shutdown via ctx cancel — not an error condition.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			wrapped := fmt.Errorf("policycache: WaitForNotification: %w", err)
			onError(wrapped)
			return wrapped
		}
		if n == nil {
			// Defensive: pgx should never return (nil, nil), but handle it.
			continue
		}
		// Filter: only deliver notifications on the channel we subscribed to.
		// pgx in principle could surface notifications from other LISTENs on
		// the same conn; we want strict channel scoping.
		if n.Channel != channel {
			continue
		}
		onNotify(n.Payload)
	}
}
