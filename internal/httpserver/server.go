package httpserver

import (
	"context"
	"io/fs"
	"net/http"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"

	"github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1/leaguelensv1connect"
)

// NewHandler mounts health checks, API services, and an optional embedded frontend.
// A nil service is not mounted; a nil static filesystem disables frontend serving.
func NewHandler(
	ping func(context.Context) error,
	dashboard leaguelensv1connect.DashboardServiceHandler,
	sync leaguelensv1connect.SyncServiceHandler,
	connections leaguelensv1connect.ConnectionServiceHandler,
	assistant leaguelensv1connect.AssistantServiceHandler,
	static fs.FS,
) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unavailable"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})

	connectMux := http.NewServeMux()
	if dashboard != nil || sync != nil || connections != nil || assistant != nil {
		server := connect.NewServer()
		if dashboard != nil {
			leaguelensv1connect.RegisterDashboardServiceHandler(server, dashboard)
		}
		if sync != nil {
			leaguelensv1connect.RegisterSyncServiceHandler(server, sync)
		}
		if connections != nil {
			leaguelensv1connect.RegisterConnectionServiceHandler(server, connections)
		}
		if assistant != nil {
			leaguelensv1connect.RegisterAssistantServiceHandler(server, assistant)
		}
		connecthttp.Mount(connectMux, server, connecthttp.WithReadMaxBytes(64<<10))
	}
	mux.Handle("/api/", chatWriteDeadlineMiddleware(http.StripPrefix("/api", connectMux)))
	if static != nil {
		mux.Handle("/", staticHandler(static))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}
