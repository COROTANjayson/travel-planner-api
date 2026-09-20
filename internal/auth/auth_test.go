package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/auth0/go-jwt-middleware/v3/validator"
	"github.com/go-chi/chi/v5"
)

const (
	testIssuer   = "https://issuer.example/"
	testAudience = "travel-planner-api"
	testSecret   = "test-secret"
)

type memoryStore struct {
	mu    sync.Mutex
	users map[string]User
}

func (s *memoryStore) GetOrCreate(_ context.Context, subject string, email *string, name string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if user, ok := s.users[subject]; ok {
		return user, nil
	}
	now := time.Now().UTC()
	user := User{ID: int64(len(s.users) + 1), Email: email, DisplayName: name, CreatedAt: now, UpdatedAt: now}
	s.users[subject] = user
	return user, nil
}

func testAuth(t *testing.T, store Store) *Auth {
	t.Helper()
	v, err := validator.New(
		validator.WithKeyFunc(func(context.Context) (any, error) { return []byte(testSecret), nil }),
		validator.WithAlgorithm(validator.HS256),
		validator.WithIssuer(testIssuer),
		validator.WithAudience(testAudience),
		validator.WithCustomClaims(func() *claims { return &claims{} }),
	)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := newAuth(v, store)
	if err != nil {
		t.Fatal(err)
	}
	return auth
}

func signedToken(t *testing.T, values map[string]any) string {
	t.Helper()
	encode := func(value any) string {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(data)
	}
	unsigned := encode(map[string]string{"alg": "HS256", "typ": "JWT"}) + "." + encode(values)
	mac := hmac.New(sha256.New, []byte(testSecret))
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func request(handler http.Handler, token string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/me", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	handler.ServeHTTP(w, r)
	return w
}

func TestAuthenticationAndRegistration(t *testing.T) {
	store := &memoryStore{users: make(map[string]User)}
	router := chi.NewRouter()
	router.Use(testAuth(t, store).Middleware)
	Register(router)

	token := signedToken(t, map[string]any{
		"iss": testIssuer, "aud": testAudience, "sub": "auth0|123",
		"exp": time.Now().Add(time.Hour).Unix(), "email": "traveler@example.com", "name": "Traveler",
	})
	for range 2 {
		w := request(router, token)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "auth_subject") {
			t.Fatal("external subject leaked")
		}
	}
	if len(store.users) != 1 {
		t.Fatalf("created %d users", len(store.users))
	}

	withoutProfile := signedToken(t, map[string]any{
		"iss": testIssuer, "aud": testAudience, "sub": "auth0|456", "exp": time.Now().Add(time.Hour).Unix(),
	})
	w := request(router, withoutProfile)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"email":null`) || !strings.Contains(w.Body.String(), `"display_name":""`) {
		t.Fatalf("optional profile claims: status %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthenticationFailures(t *testing.T) {
	auth := testAuth(t, &memoryStore{users: make(map[string]User)})
	router := chi.NewRouter()
	router.Use(auth.Middleware)
	Register(router)

	cases := map[string]string{
		"missing":         "",
		"malformed":       "not-a-token",
		"expired":         signedToken(t, map[string]any{"iss": testIssuer, "aud": testAudience, "sub": "user", "exp": time.Now().Add(-time.Hour).Unix()}),
		"missing subject": signedToken(t, map[string]any{"iss": testIssuer, "aud": testAudience, "exp": time.Now().Add(time.Hour).Unix()}),
		"wrong issuer":    signedToken(t, map[string]any{"iss": "https://wrong.example/", "aud": testAudience, "sub": "user", "exp": time.Now().Add(time.Hour).Unix()}),
		"wrong audience":  signedToken(t, map[string]any{"iss": testIssuer, "aud": "wrong", "sub": "user", "exp": time.Now().Add(time.Hour).Unix()}),
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			w := request(router, token)
			if w.Code != http.StatusUnauthorized || w.Body.String() != "{\"error\":\"unauthorized\"}\n" {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
}
