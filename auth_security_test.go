package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestLoginRequiresExistingUserAndNonemptyPassword(t *testing.T) {
	setupAPITest(t, settings{})
	previous := appUsers
	appUsers = map[string]string{"admin": "a-strong-test-password-928!", "empty": ""}
	t.Cleanup(func() { appUsers = previous })
	for _, tc := range []struct {
		name, user, password string
		status               int
	}{
		{"unknown empty", "unknown", "", http.StatusUnauthorized},
		{"unknown omitted", "unknown", "", http.StatusUnauthorized},
		{"unknown password", "unknown", "a-strong-test-password-928!", http.StatusUnauthorized},
		{"empty account", "empty", "", http.StatusUnauthorized},
		{"empty username", "", "", http.StatusUnauthorized},
		{"wrong password", "admin", "admin123", http.StatusUnauthorized},
		{"correct", "admin", "a-strong-test-password-928!", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"username": tc.user, "password": tc.password}
			if tc.name == "unknown omitted" {
				delete(body, "password")
			}
			response := apiTestRequest(t, "", http.MethodPost, "/api/auth/login", body)
			if response.Code != tc.status {
				t.Fatalf("got status %d, want %d", response.Code, tc.status)
			}
			var data map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
				t.Fatal(err)
			}
			if tc.status == http.StatusUnauthorized && data["token"] != nil {
				t.Fatal("rejected login returned a token")
			}
			if tc.status == http.StatusOK {
				token, ok := data["token"].(string)
				if !ok {
					t.Fatal("valid login omitted token")
				}
				user, err := verifyToken(token)
				if err != nil || user != "admin" {
					t.Fatal("valid login returned invalid token")
				}
			}
		})
	}
}
