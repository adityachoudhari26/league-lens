package httpserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestChatWriteDeadlineIsExtendedOnlyForChat(t *testing.T) {
	server := httptest.NewUnstartedServer(chatWriteDeadlineMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte("completed"))
	})))
	server.Config.WriteTimeout = 30 * time.Millisecond
	server.Start()
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = time.Second

	response, err := client.Get(server.URL + assistantChatPath)
	if err != nil {
		t.Fatalf("Chat request failed: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || string(body) != "completed" {
		t.Fatalf("Chat response = status %d, body %q, read error %v", response.StatusCode, body, err)
	}

	response, err = client.Get(server.URL + "/api/leaguelens.v1.AssistantService/GetAssistantSettings")
	if err == nil {
		body, _ = io.ReadAll(response.Body)
		_ = response.Body.Close()
		if string(body) == "completed" {
			t.Fatal("non-Chat path completed after the configured write timeout")
		}
	}
}
