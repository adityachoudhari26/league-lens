package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1/leaguelensv1connect"
)

type assistantHandlerStub struct {
	leaguelensv1connect.UnimplementedAssistantServiceHandler
}

func (assistantHandlerStub) GetAssistantSettings(context.Context, *v1.GetAssistantSettingsRequest) (*v1.GetAssistantSettingsResponse, error) {
	return &v1.GetAssistantSettingsResponse{Settings: &v1.AssistantSettings{Model: "test-model"}}, nil
}

func TestReadinessFailureDoesNotLeakDatabaseError(t *testing.T) {
	h := NewHandler(func(context.Context) error { return errors.New("password=private-secret") }, nil, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/readyz", nil))
	if rr.Code != 503 {
		t.Fatalf("status = %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "private-secret") {
		t.Fatal("leaked DB error")
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/healthz", nil))
	if rr.Code != 200 {
		t.Fatalf("liveness depends on database: %d", rr.Code)
	}
}

func TestReadyAndUnregisteredEndpoints(t *testing.T) {
	h := NewHandler(func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("DB check has no deadline")
		}
		return nil
	}, nil, nil, nil, nil, nil)
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/readyz", 200}, {"POST", "/readyz", 405}, {"GET", "/api/teams", 404},
	} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(tc.method, tc.path, nil))
		if rr.Code != tc.status {
			t.Errorf("%s %s: %d", tc.method, tc.path, rr.Code)
		}
	}
}

func TestAssistantServiceIsMounted(t *testing.T) {
	handler := NewHandler(
		func(context.Context) error { return nil }, nil, nil, nil, assistantHandlerStub{}, nil,
	)
	request := httptest.NewRequest(http.MethodPost, "/api/leaguelens.v1.AssistantService/GetAssistantSettings", strings.NewReader("{}"))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"model":"test-model"`) {
		t.Fatalf("assistant settings RPC: status %d, body %s", response.Code, response.Body.String())
	}
}

func TestEmbeddedFrontendServesFilesAndClientRoutes(t *testing.T) {
	static := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>app</html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('app')")},
	}
	h := NewHandler(func(context.Context) error { return nil }, nil, nil, nil, nil, static)
	for _, tc := range []struct {
		path, body, cacheControl string
		status                   int
	}{
		{path: "/", body: "<html>app</html>", cacheControl: "no-cache", status: http.StatusOK},
		{path: "/matchups", body: "<html>app</html>", cacheControl: "no-cache", status: http.StatusOK},
		{path: "/assets/app.js", body: "console.log('app')", cacheControl: "public, max-age=31536000, immutable", status: http.StatusOK},
		{path: "/assets/missing.js", status: http.StatusNotFound},
		{path: "/api/unknown", status: http.StatusNotFound},
	} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rr.Code != tc.status {
			t.Errorf("%s: status = %d, want %d", tc.path, rr.Code, tc.status)
		}
		if tc.body != "" && rr.Body.String() != tc.body {
			t.Errorf("%s: body = %q, want %q", tc.path, rr.Body.String(), tc.body)
		}
		if tc.cacheControl != "" && rr.Header().Get("Cache-Control") != tc.cacheControl {
			t.Errorf("%s: Cache-Control = %q, want %q", tc.path, rr.Header().Get("Cache-Control"), tc.cacheControl)
		}
		if tc.path == "/api/unknown" && strings.Contains(rr.Body.String(), "<html>") {
			t.Fatal("unknown API path fell back to the frontend")
		}
	}
}

func TestRequireLocalHost(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := RequireLocalHost(next)
	for _, tc := range []struct {
		host string
		want int
	}{
		{host: "localhost:5173", want: http.StatusNoContent},
		{host: "league-lens.localhost", want: http.StatusNoContent},
		{host: "LEAGUE-LENS.LOCALHOST:8080", want: http.StatusNoContent},
		{host: "127.0.0.1:8080", want: http.StatusNoContent},
		{host: "[::1]:8080", want: http.StatusNoContent},
		{host: "evil.com", want: http.StatusForbidden},
		{host: "localhost.evil.com", want: http.StatusForbidden},
		{host: "127.0.0.1.nip.io", want: http.StatusForbidden},
		{host: "", want: http.StatusForbidden},
	} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = tc.host
		handler.ServeHTTP(rr, req)
		if rr.Code != tc.want {
			t.Errorf("Host %q: status = %d, want %d", tc.host, rr.Code, tc.want)
		}
	}
}
