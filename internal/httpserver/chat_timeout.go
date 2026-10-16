package httpserver

import (
	"net/http"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1/leaguelensv1connect"
)

const (
	assistantChatPath = "/api" + leaguelensv1connect.AssistantServiceChatProcedure
	// chatWriteTimeout must exceed the assistant's 2-minute chatTimeout.
	chatWriteTimeout = 3 * time.Minute
)

// chatWriteDeadlineMiddleware extends writes for streamed assistant chat responses.
func chatWriteDeadlineMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == assistantChatPath {
			deadline := time.Now().Add(chatWriteTimeout)
			if err := http.NewResponseController(w).SetWriteDeadline(deadline); err != nil {
				http.Error(w, "assistant streaming is unavailable", http.StatusInternalServerError)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
