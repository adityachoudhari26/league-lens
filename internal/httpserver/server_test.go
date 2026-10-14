package httpserver

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadinessFailureDoesNotLeakDatabaseError(t *testing.T) {
	h := NewHandler(func(context.Context) error { return errors.New("password=private-secret") }, nil, nil)
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
	}, nil, nil)
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
