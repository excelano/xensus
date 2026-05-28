package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// ctxKey is the unexported type for context keys set in this package, so a
// value stored here can't collide with one set elsewhere under the same name.
type ctxKey int

const requestIDKey ctxKey = iota

// newRequestID returns a random UUIDv4 string. It reads from crypto/rand
// directly rather than pulling in a UUID dependency — a request id only needs
// to be unique enough to correlate log lines, and the v4 layout reads
// familiarly in logs and headers.
func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand should never fail; if it somehow does, fall back to a
		// timestamp so logging still gets a non-empty correlation token.
		return fmt.Sprintf("ts-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// requestIDFrom returns the request id stored in ctx by the requestID
// middleware, if one is present.
func requestIDFrom(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(requestIDKey).(string)
	return id, ok
}

// statusRecorder wraps a ResponseWriter to remember the status code, so the
// completion log can report it. It defaults to 200: a handler that writes a
// body without calling WriteHeader has implicitly sent 200.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (rec *statusRecorder) WriteHeader(code int) {
	rec.status = code
	rec.ResponseWriter.WriteHeader(code)
}

// requestID is the outermost middleware. It stamps each request with a unique
// id, exposes it on the X-Request-Id response header and in the request
// context (so any slog ...Context call inside a handler carries it via
// contextHandler), and logs one line when the request completes.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r.WithContext(ctx))
		slog.InfoContext(ctx, "request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"dur_ms", time.Since(start).Milliseconds(),
		)
	})
}

// contextHandler decorates a slog.Handler so every record carries the request
// id from its context when one is present. Startup logs, which have no request
// context, simply omit the attribute.
type contextHandler struct{ slog.Handler }

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id, ok := requestIDFrom(ctx); ok {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}
