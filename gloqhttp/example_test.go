package gloqhttp_test

import (
	"log/slog"
	"net/http"

	"github.com/skinleak/gloq"
	"github.com/skinleak/gloq/gloqhttp"
)

func ExampleMiddleware() {
	// Add the request ID to every record logged with a request's context.
	log := gloq.NewLogger(gloq.WithContextAttrs(gloqhttp.ContextAttrs))

	mux := http.NewServeMux()
	mux.HandleFunc("/orders", func(w http.ResponseWriter, r *http.Request) {
		log.InfoContext(r.Context(), "listing orders")
		w.WriteHeader(http.StatusOK)
	})

	middleware := gloqhttp.Middleware(log.Logger, gloqhttp.WithSkip(func(r *http.Request) bool {
		return r.URL.Path == "/healthz"
	}))
	server := &http.Server{Addr: ":8080", Handler: middleware(mux)}
	_ = server // server.ListenAndServe()
}

func ExampleRequestID() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Pass the ID on to downstream services.
		request, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, "http://inventory/items", nil)
		request.Header.Set(gloqhttp.DefaultRequestIDHeader, gloqhttp.RequestID(r.Context()))
	})
	_ = gloqhttp.Middleware(slog.Default())(handler)
}
