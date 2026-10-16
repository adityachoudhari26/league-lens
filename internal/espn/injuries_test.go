package espn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func readInjuryFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestParseAthleteInjuriesQuestionableFixture(t *testing.T) {
	injury, err := parseAthleteInjuries(readInjuryFixture(t, "injuries_athlete_questionable.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := AthleteInjury{
		Status: "Questionable", Injury: "Hamstring", ReturnDate: "2026-10-18",
		Note:    "Johnson (hamstring) was a limited participant at practice Thursday.",
		Updated: time.Date(2026, time.October, 16, 1, 6, 0, 0, time.UTC),
	}
	if injury == nil || *injury != want {
		t.Fatalf("injury = %+v, want %+v", injury, want)
	}
}

func TestParseAthleteInjuriesInjuredReserveFixture(t *testing.T) {
	injury, err := parseAthleteInjuries(readInjuryFixture(t, "injuries_athlete_ir.json"))
	if err != nil {
		t.Fatal(err)
	}
	if injury == nil || injury.Status != "Injured Reserve" || injury.ReturnDate != "2026-10-18" || injury.Note != "ir" {
		t.Fatalf("injury = %+v", injury)
	}
}

func TestParseAthleteInjuriesEmptyListIsNoInjury(t *testing.T) {
	injury, err := parseAthleteInjuries(readInjuryFixture(t, "injuries_athlete_none.json"))
	if err != nil || injury != nil {
		t.Fatalf("injury, err = %+v, %v", injury, err)
	}
}

func TestParseAthleteInjuriesKeepsNewestDatedEntry(t *testing.T) {
	body := []byte(`{"items":[
		{"status":"Out","date":"2026-10-01T10:00Z","details":{"type":"Knee"}},
		{"status":"Questionable","date":"2026-10-12T10:00Z","details":{"type":"Ankle"}},
		{"status":"Doubtful","date":"not a date"},
		{"date":"2026-10-15T10:00Z"}
	]}`)
	injury, err := parseAthleteInjuries(body)
	if err != nil {
		t.Fatal(err)
	}
	if injury == nil || injury.Status != "Questionable" || injury.Injury != "Ankle" {
		t.Fatalf("injury = %+v", injury)
	}
}

func TestParseAthleteInjuriesRejectsInvalidJSON(t *testing.T) {
	if _, err := parseAthleteInjuries([]byte(`{"items":`)); err == nil {
		t.Fatal("expected an error")
	}
}

func TestInjuryClientRequestsPublicAthletePathAndCaches(t *testing.T) {
	fixture := readInjuryFixture(t, "injuries_athlete_questionable.json")
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/seasons/2026/athletes/3915411/injuries" || r.URL.Query().Get("limit") != "3" {
			t.Errorf("request = %s", r.URL)
		}
		if r.Header.Get("Cookie") != "" {
			t.Error("injury request sent cookies")
		}
		_, _ = w.Write(fixture)
	}))
	defer server.Close()
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	client := testInjuryClient(server.URL, func() time.Time { return now })

	for range 2 {
		injuries, err := client.AthleteInjuries(context.Background(), 2026, []int64{3915411})
		if err != nil || injuries[3915411] == nil || injuries[3915411].Status != "Questionable" {
			t.Fatalf("injuries, err = %+v, %v", injuries, err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("hits within the cache window = %d, want 1", hits.Load())
	}
	now = now.Add(injuryCacheTTL)
	if _, err := client.AthleteInjuries(context.Background(), 2026, []int64{3915411}); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Fatalf("hits after the cache expired = %d, want 2", hits.Load())
	}
}

func TestInjuryClientKeepsAtMostFourRequestsInFlight(t *testing.T) {
	var inFlight, peak atomic.Int32
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		current := inFlight.Add(1)
		mu.Lock()
		peak.Store(max(peak.Load(), current))
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()
	client := testInjuryClient(server.URL, time.Now)

	injuries, err := client.AthleteInjuries(context.Background(), 2026, []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	if err != nil || len(injuries) != 10 {
		t.Fatalf("injuries, err = %d, %v", len(injuries), err)
	}
	if peak.Load() > injuryRequestsInFlight {
		t.Fatalf("peak in-flight requests = %d", peak.Load())
	}
}

func TestInjuryClientRetriesAFailedAthleteOnce(t *testing.T) {
	var athleteTwoHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/athletes/2/") && athleteTwoHits.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		_, _ = w.Write(readInjuryFixture(t, "injuries_athlete_questionable.json"))
	}))
	defer server.Close()
	client := testInjuryClient(server.URL, time.Now)

	injuries, err := client.AthleteInjuries(context.Background(), 2026, []int64{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(injuries) != 2 || injuries[2] == nil || injuries[2].Status != "Questionable" {
		t.Fatalf("injuries = %+v", injuries)
	}
	if athleteTwoHits.Load() != 2 {
		t.Fatalf("athlete 2 requests = %d, want 2", athleteTwoHits.Load())
	}
}

func TestInjuryClientFailsWhenAnAthleteKeepsFailing(t *testing.T) {
	var athleteTwoHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/athletes/2/") {
			athleteTwoHits.Add(1)
			http.Error(w, "secret upstream body", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()
	client := testInjuryClient(server.URL, time.Now)

	injuries, err := client.AthleteInjuries(context.Background(), 2026, []int64{1, 2})
	if err == nil || injuries != nil {
		t.Fatalf("injuries, err = %+v, %v; want an error", injuries, err)
	}
	if strings.Contains(err.Error(), "secret upstream body") {
		t.Fatalf("error leaked the response body: %v", err)
	}
	if athleteTwoHits.Load() != 2 {
		t.Fatalf("athlete 2 requests = %d, want 2", athleteTwoHits.Load())
	}
}

func TestInjuryClientTreatsNotFoundAsNoInjuryWithoutRetry(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.NotFound(w, nil)
	}))
	defer server.Close()
	client := testInjuryClient(server.URL, time.Now)

	injuries, err := client.AthleteInjuries(context.Background(), 2026, []int64{7})
	if err != nil {
		t.Fatal(err)
	}
	if injury, found := injuries[7]; !found || injury != nil {
		t.Fatalf("injuries = %+v, want athlete 7 with no injury", injuries)
	}
	if hits.Load() != 1 {
		t.Fatalf("requests = %d, want 1", hits.Load())
	}
}

func TestInjuryClientDoesNotRetryCancelledRequests(t *testing.T) {
	var hits atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		cancel()
		http.Error(w, "late", http.StatusInternalServerError)
	}))
	defer server.Close()
	client := testInjuryClient(server.URL, time.Now)

	if _, err := client.AthleteInjuries(ctx, 2026, []int64{7}); err == nil {
		t.Fatal("expected an error")
	}
	if hits.Load() != 1 {
		t.Fatalf("requests = %d, want 1", hits.Load())
	}
}

func testInjuryClient(baseURL string, now func() time.Time) *InjuryClient {
	client := NewInjuryClient(nil)
	client.baseURL = baseURL
	client.now = now
	return client
}

func TestParseAthleteInjuriesKeepsStaleEntryForCallerToJudge(t *testing.T) {
	injury, err := parseAthleteInjuries(readInjuryFixture(t, "injuries_athlete_stale_questionable.json"))
	if err != nil {
		t.Fatal(err)
	}
	if injury == nil || injury.Status != "Questionable" ||
		!injury.Updated.Equal(time.Date(2026, time.September, 8, 20, 42, 0, 0, time.UTC)) {
		t.Fatalf("injury = %+v", injury)
	}
}
