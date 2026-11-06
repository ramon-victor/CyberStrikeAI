package handler

import (
	"errors"
	"net/http"
	"testing"
)

func TestMultiAgentErrorStatusSupportsEnglishAndLegacyMessages(t *testing.T) {
	for _, tc := range []struct {
		message string
		status  int
	}{
		{"no permission to access this conversation", http.StatusForbidden},
		{"Conversation not found", http.StatusNotFound},
		{"Failed to save user message: database unavailable", http.StatusInternalServerError},
		{"Failed to create conversation: database unavailable", http.StatusInternalServerError},
		{"Failed to save uploaded file: disk full", http.StatusInternalServerError},
		{"Maximum 5 attachment(s)", http.StatusBadRequest},
		{"无权访问", http.StatusForbidden},
		{"对话不存在", http.StatusNotFound},
	} {
		status, message := multiAgentHTTPErrorStatus(errors.New(tc.message))
		if status != tc.status || message != tc.message {
			t.Errorf("%q: status=%d message=%q", tc.message, status, message)
		}
	}
}
