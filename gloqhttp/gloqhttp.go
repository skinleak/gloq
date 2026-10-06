// Package gloqhttp logs HTTP requests and gives each request an ID.
//
// [Middleware] writes one record per request, with its method, path, status,
// response size, and duration, at INFO, WARN for 4xx responses, or ERROR for
// 5xx responses and panics. It works with any [*slog.Logger], not only gloq.
//
//	log := gloq.New(gloq.WithContextAttrs(gloqhttp.ContextAttrs))
//	http.ListenAndServe(":8080", gloqhttp.Middleware(log)(mux))
//
// Each request gets an ID, taken from its X-Request-ID header when present
// and generated otherwise, which is echoed in the response. Handlers can read
// it with [RequestID]. Passing [ContextAttrs] to [gloq.WithContextAttrs], as
// above, adds it to every record logged with the request's context, so all
// lines from one request can be found together.
//
// [gloq.WithContextAttrs]: https://pkg.go.dev/github.com/skinleak/gloq#WithContextAttrs
package gloqhttp

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// RequestIDKey is the attribute key used for request IDs.
const RequestIDKey = "request_id"

// DefaultRequestIDHeader is the header request IDs are read from and written
// to unless [WithRequestIDHeader] says otherwise.
const DefaultRequestIDHeader = "X-Request-ID"

type config struct {
	header string
	skip   func(*http.Request) bool
}

// Option changes how [Middleware] works.
type Option func(*config)

// WithRequestIDHeader sets the header that carries request IDs. An empty
// name ignores incoming IDs, always generates a new one, and does not echo
// it in the response.
func WithRequestIDHeader(name string) Option {
	return func(c *config) { c.header = name }
}

// WithSkip leaves requests for which skip returns true unlogged, such as
// health checks. They still get a request ID.
func WithSkip(skip func(*http.Request) bool) Option {
	return func(c *config) { c.skip = skip }
}

type requestIDKey struct{}

// accessLogKey marks the context of the middleware's own record, which
// already has the request ID.
type accessLogKey struct{}

// RequestID returns the ID that [Middleware] gave the request ctx belongs to,
// or "" if there is none.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// ContextAttrs returns the request ID in ctx as an attribute. Pass it to
// gloq.WithContextAttrs to add the ID to every record logged with a request's
// context.
func ContextAttrs(ctx context.Context) []slog.Attr {
	id := RequestID(ctx)
	if id == "" || ctx.Value(accessLogKey{}) != nil {
		return nil
	}
	return []slog.Attr{slog.String(RequestIDKey, id)}
}

// Middleware returns a function that wraps an [http.Handler] to log every
// request to logger once its response is complete. A handler that panics is
// logged at ERROR with the panic value, and the panic continues as before.
func Middleware(logger *slog.Logger, options ...Option) func(http.Handler) http.Handler {
	if logger == nil {
		panic("gloqhttp: nil logger")
	}
	c := config{header: DefaultRequestIDHeader}
	for _, option := range options {
		if option != nil {
			option(&c)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			id := ""
			if c.header != "" {
				id = r.Header.Get(c.header)
			}
			if !validRequestID(id) {
				id = newRequestID()
			}
			if c.header != "" {
				w.Header().Set(c.header, id)
			}
			ctx := context.WithValue(r.Context(), requestIDKey{}, id)
			r = r.WithContext(ctx)

			if c.skip != nil && c.skip(r) {
				next.ServeHTTP(w, r)
				return
			}

			recorder := &responseRecorder{ResponseWriter: w}
			completed := false
			defer func() {
				if completed {
					return
				}
				recovered := recover()
				if recovered == nil {
					// The handler called runtime.Goexit; let it continue.
					return
				}
				// ErrAbortHandler is how handlers abort a response on purpose.
				if recovered != http.ErrAbortHandler {
					logRequest(ctx, logger, r, recorder, id, start, recovered)
				}
				panic(recovered)
			}()
			next.ServeHTTP(recorder, r)
			completed = true
			logRequest(ctx, logger, r, recorder, id, start, nil)
		})
	}
}

func logRequest(ctx context.Context, logger *slog.Logger, r *http.Request, recorder *responseRecorder, id string, start time.Time, recovered any) {
	status := recorder.status
	if status == 0 && recovered == nil {
		// Nothing was written, so net/http sends an empty 200 response.
		status = http.StatusOK
	}
	level := slog.LevelInfo
	switch {
	case recovered != nil || status >= 500:
		level = slog.LevelError
	case status >= 400:
		level = slog.LevelWarn
	}
	ctx = context.WithValue(ctx, accessLogKey{}, true)
	if !logger.Enabled(ctx, level) {
		return
	}

	attrs := make([]slog.Attr, 0, 8)
	attrs = append(attrs,
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", status),
		slog.Int64("bytes", recorder.bytes),
		slog.Duration("duration", time.Since(start)),
		slog.String(RequestIDKey, id),
	)
	if recovered != nil {
		attrs = append(attrs, slog.Any("panic", recovered))
	}
	logger.LogAttrs(ctx, level, "http request", attrs...)
}

// validRequestID accepts IDs of up to 128 printable ASCII characters other
// than spaces, so a client cannot fill logs or headers with arbitrary data.
func validRequestID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for index := 0; index < len(id); index++ {
		if id[index] <= ' ' || id[index] > '~' {
			return false
		}
	}
	return true
}

func newRequestID() string {
	var id [16]byte
	_, _ = rand.Read(id[:])
	return hex.EncodeToString(id[:])
}

// responseRecorder records the status and size of a response. It keeps the
// optional interfaces of the writer it wraps reachable through Unwrap, which
// [http.ResponseController] uses, and implements the common ones directly.
type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *responseRecorder) WriteHeader(status int) {
	// Informational responses are followed by the real one.
	if w.status == 0 && (status < 100 || status >= 200) {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	written, err := w.ResponseWriter.Write(data)
	w.bytes += int64(written)
	return written, err
}

// ReadFrom keeps io.Copy able to use sendfile, such as for http.ServeFile.
func (w *responseRecorder) ReadFrom(source io.Reader) (int64, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	var written int64
	var err error
	if from, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		written, err = from.ReadFrom(source)
	} else {
		written, err = io.Copy(writerOnly{w.ResponseWriter}, source)
	}
	w.bytes += written
	return written, err
}

func (w *responseRecorder) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffer, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil && w.status == 0 {
		w.status = http.StatusSwitchingProtocols
	}
	return conn, buffer, err
}

func (w *responseRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// writerOnly hides a writer's ReadFrom method so io.Copy does not recurse.
type writerOnly struct {
	io.Writer
}
