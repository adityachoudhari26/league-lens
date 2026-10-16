package httpserver

import (
	"net/http"
	"time"
)

const assistantChatPath = "/api/leaguelens.v1.AssistantService/Chat"

func chatWriteDeadlineMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == assistantChatPath {
			deadline := time.Now().Add(3 * time.Minute)
			if err := http.NewResponseController(w).SetWriteDeadline(deadline); err != nil {
				http.Error(w, "assistant streaming is unavailable", http.StatusInternalServerError)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
