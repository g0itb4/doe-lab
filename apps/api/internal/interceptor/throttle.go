package interceptor

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
)

// Throttle rations requests per client address with a token bucket. It is
// in-process on purpose: one API instance, and state that dies with the
// process is the right durability for a rate limit.
//
// A device's stream counts once, when it opens; the messages on it do not.
type Throttle struct {
	clientOnly
	mu         sync.Mutex
	buckets    map[string]*bucket
	burst      float64
	perSecond  float64
	trustProxy bool
	now        func() time.Time
}

// Generous for a demo nobody is attacking, and still far below what it takes
// to hurt the box. The burst covers a page that opens several panels at once,
// and a fleet of simulated devices that all subscribe from one address.
const (
	defaultBurst     = 400
	defaultPerSecond = 100

	// A backstop, not a working set. Reaching it means something is spraying
	// addresses, which is exactly when the map must not be what falls over.
	throttleMax = 8192
)

// NewThrottle builds the limiter. trustProxy says whether X-Forwarded-For is
// evidence (an edge in front appends the address it saw) or input.
func NewThrottle(trustProxy bool) *Throttle {
	return &Throttle{
		buckets: make(map[string]*bucket), burst: defaultBurst, perSecond: defaultPerSecond,
		trustProxy: trustProxy, now: time.Now,
	}
}

// bucket is a token bucket held as a level plus the time it was last
// refilled, so an idle key costs no ticker and no goroutine.
type bucket struct {
	tokens float64
	last   time.Time
}

// allow spends one token from the bucket of key.
func (t *Throttle) allow(key string) bool {
	now := t.now()

	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.buckets) >= throttleMax {
		t.evictFull(now)
	}
	b, ok := t.buckets[key]
	if !ok {
		b = &bucket{tokens: t.burst, last: now}
		t.buckets[key] = b
	}
	b.tokens = min(t.burst, b.tokens+now.Sub(b.last).Seconds()*t.perSecond)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// evictFull drops the buckets that have refilled, since a full bucket is the
// same as one that never existed. Called with the lock held.
func (t *Throttle) evictFull(now time.Time) {
	for k, b := range t.buckets {
		if b.tokens+now.Sub(b.last).Seconds()*t.perSecond >= t.burst {
			delete(t.buckets, k)
		}
	}
	// Still full of live limiters: every one is being spent, which is the
	// attack this exists for. Drop the lot rather than grow without bound.
	if len(t.buckets) >= throttleMax {
		clear(t.buckets)
	}
}

// Deliberately says nothing about how long is left: that is tuning
// information for whoever is probing.
var errThrottled = connect.NewError(connect.CodeResourceExhausted, errors.New("too many requests"))

// WrapUnary spends one token per call.
func (t *Throttle) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if !t.allow(ClientIP(req.Peer().Addr, req.Header(), t.trustProxy)) {
			return nil, errThrottled
		}
		return next(ctx, req)
	}
}

// WrapStreamingHandler spends one token when a stream opens.
func (t *Throttle) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if !t.allow(ClientIP(conn.Peer().Addr, conn.RequestHeader(), t.trustProxy)) {
			return errThrottled
		}
		return next(ctx, conn)
	}
}

// ClientIP is the address to attribute a request to. X-Forwarded-For is
// honoured only when trustProxy says an edge is in front of the API, and only
// its LAST entry: the edge appends the address it vouches for, so earlier
// entries are the caller's own claim.
func ClientIP(peerAddr string, h http.Header, trustProxy bool) string {
	if trustProxy {
		if xff := h.Get("X-Forwarded-For"); xff != "" {
			last := xff
			if i := strings.LastIndex(xff, ","); i >= 0 {
				last = xff[i+1:]
			}
			if last = strings.TrimSpace(last); last != "" {
				return last
			}
		}
	}
	if host, _, err := net.SplitHostPort(peerAddr); err == nil {
		return host
	}
	return peerAddr
}
