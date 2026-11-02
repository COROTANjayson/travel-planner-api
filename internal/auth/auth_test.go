package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
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

func (s *memoryStore) GetOrCreate(_ context.Context, subject string, email *string, emailVerified bool, name string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if user, ok := s.users[subject]; ok {
		user.Email, user.EmailVerified = email, emailVerified
		s.users[subject] = user
		return user, nil
	}
	now := time.Now().UTC()
	user := User{ID: int64(len(s.users) + 1), Email: email, EmailVerified: emailVerified, DisplayName: name, CreatedAt: now, UpdatedAt: now}
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
		"exp": time.Now().Add(time.Hour).Unix(), "email": "traveler@example.com", "email_verified": true, "name": "Traveler",
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
	if !store.users["auth0|123"].EmailVerified {
		t.Fatal("email verification claim was not persisted")
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMissingEmailUsesVerifiedUserInfo(t *testing.T) {
	store := &memoryStore{users: make(map[string]User)}
	a := testAuth(t, store)
	router := chi.NewRouter()
	router.Use(a.Middleware)
	Register(router)
	token := signedToken(t, map[string]any{
		"iss": testIssuer, "aud": testAudience, "sub": "auth0|profile",
		"exp": time.Now().Add(time.Hour).Unix(), "scope": "openid profile email",
	})
	calls := 0
	previous := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != testIssuer+"userinfo" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("profile request did not use the validated issuer and access token")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"sub":"auth0|profile","email":"traveler@example.com","email_verified":true,"name":"Traveler"}`)), Header: make(http.Header)}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	for range 3 {
		w := request(router, token)
		var user User
		if err := json.Unmarshal(w.Body.Bytes(), &user); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || user.Email == nil || *user.Email != "traveler@example.com" || !user.EmailVerified {
			t.Fatalf("verified provider profile was lost: status %d, email present %v, verified %v", w.Code, user.Email != nil, user.EmailVerified)
		}
	}
	if calls != 1 {
		t.Fatalf("same-token profile fetched %d times, want one cached lookup", calls)
	}
}

func TestUserInfoFailuresDoNotProvisionUsers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   int
	}{
		{"wrong subject", 200, `{"sub":"another-user","email":"other@example.com","email_verified":true}`, 401},
		{"missing subject", 200, `{"email":"other@example.com","email_verified":true}`, 401},
		{"provider rejects token", 401, `{}`, 401},
		{"rate limit", 429, `{}`, 503},
		{"outage", 500, `private provider payload`, 503},
		{"redirect", 302, `{}`, 503},
		{"malformed", 200, `{`, 503},
		{"oversized", 200, strings.Repeat(" ", 64<<10) + `{}`, 503},
		{"oversized email", 200, `{"sub":"auth0|profile","email":"` + strings.Repeat("a", 321) + `"}`, 503},
		{"null character", 200, `{"sub":"auth0|profile","email":"person\u0000@example.com"}`, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &memoryStore{users: make(map[string]User)}
			a := testAuth(t, store)
			router := chi.NewRouter()
			router.Use(a.Middleware)
			Register(router)
			calls := 0
			previous := http.DefaultTransport
			http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "issuer.example" {
					t.Error("access token forwarded outside configured issuer")
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{"Location": []string{"https://other.example/userinfo"}}}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = previous })
			token := signedToken(t, map[string]any{"iss": testIssuer, "aud": testAudience, "sub": "auth0|profile", "exp": time.Now().Add(time.Hour).Unix(), "scope": "openid email"})
			w := request(router, token)
			if w.Code != tc.want || len(store.users) != 0 || calls != 1 {
				t.Fatalf("status %d want %d, users %d, calls %d", w.Code, tc.want, len(store.users), calls)
			}
			if strings.Contains(w.Body.String(), "private provider payload") || strings.Contains(w.Body.String(), token) {
				t.Fatal("private provider data leaked")
			}
		})
	}
}

func TestProfileCacheRefreshAndIdentityScope(t *testing.T) {
	store := &memoryStore{users: make(map[string]User)}
	a := testAuth(t, store)
	router := chi.NewRouter()
	router.Use(a.Middleware)
	Register(router)
	verified, calls := false, 0
	previous := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		body, _ := json.Marshal(map[string]any{"sub": "auth0|profile", "email": "traveler@example.com", "email_verified": verified})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	values := map[string]any{"iss": testIssuer, "aud": testAudience, "sub": "auth0|profile", "exp": time.Now().Add(time.Hour).Unix(), "scope": "openid email"}
	oldToken := signedToken(t, values)
	if request(router, oldToken).Code != 200 || store.users["auth0|profile"].EmailVerified {
		t.Fatal("unverified email was incorrectly verified")
	}
	verified = true
	request(router, oldToken)
	if calls != 1 || store.users["auth0|profile"].EmailVerified {
		t.Fatal("cache did not retain the old token's profile")
	}
	values["iat"] = time.Now().Unix()
	newToken := signedToken(t, values)
	if request(router, newToken).Code != 200 || !store.users["auth0|profile"].EmailVerified || calls != 2 {
		t.Fatal("fresh sign-in did not refresh verification")
	}
	key := sha256.Sum256([]byte("Bearer " + oldToken))
	a.profileMu.Lock()
	entry := a.profiles[key]
	entry.expires = time.Now().Add(-time.Second)
	a.profiles[key] = entry
	a.profileMu.Unlock()
	request(router, oldToken)
	if calls != 3 || !store.users["auth0|profile"].EmailVerified {
		t.Fatal("expired cache did not refresh")
	}
	a.profileMu.Lock()
	for i := 0; len(a.profiles) < 256; i++ {
		a.profiles[sha256.Sum256([]byte(fmt.Sprint(i)))] = cachedProfile{}
	}
	a.profileMu.Unlock()
	values["jti"] = "next-token"
	if request(router, signedToken(t, values)).Code != 200 || len(a.profiles) != 1 {
		t.Fatal("profile cache grew beyond its bound")
	}
}
