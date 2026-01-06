package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"google.golang.org/api/idtoken"
)

func TestVerifyGoogleTokenRequiresClientID(t *testing.T) {
	old := os.Getenv("GOOGLE_CLIENT_ID")
	_ = os.Unsetenv("GOOGLE_CLIENT_ID")
	t.Cleanup(func() {
		_ = os.Setenv("GOOGLE_CLIENT_ID", old)
	})

	_, err := verifyGoogleToken(context.Background(), "dummy")
	if err == nil {
		t.Fatalf("expected error when GOOGLE_CLIENT_ID is unset")
	}
}

func TestGoogleLoginRequiresLinkedAccount(t *testing.T) {
	usersMutex.Lock()
	users = nil
	users = append(users, User{
		"username": "alice",
		"email":    "alice@example.com",
		"password": "x",
		"key":      "k",
	})
	usersMutex.Unlock()

	oldVerify := verifyGoogleToken
	verifyGoogleToken = func(ctx context.Context, token string) (*idtoken.Payload, error) {
		return &idtoken.Payload{
			Subject: "google-sub-123",
			Claims:  map[string]any{"email": "alice@example.com", "email_verified": true},
		}, nil
	}
	t.Cleanup(func() { verifyGoogleToken = oldVerify })

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/auth/google", handleUserGoogle)

	req := httptest.NewRequest(http.MethodPost, "/auth/google", strings.NewReader(`{"id_token":"dummy","system":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unlinked account, got %d: %s", w.Code, w.Body.String())
	}
}
