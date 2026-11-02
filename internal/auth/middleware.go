package auth

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	jwtmiddleware "github.com/auth0/go-jwt-middleware/v3"
	"github.com/auth0/go-jwt-middleware/v3/validator"
	"travel-planner/travel-planner-api/internal/httpx"
)

type claims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Scope         string `json:"scope"`
}

func (*claims) Validate(context.Context) error { return nil }

type userKey struct{}

func CurrentUser(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(userKey{}).(User)
	return user, ok
}

func WithUser(ctx context.Context, user User) context.Context {
	return context.WithValue(ctx, userKey{}, user)
}

func (a *Auth) Middleware(next http.Handler) http.Handler {
	return a.jwt.CheckJWT(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		validated, err := jwtmiddleware.GetClaims[*validator.ValidatedClaims](r.Context())
		if err != nil {
			httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		subject := strings.TrimSpace(validated.RegisteredClaims.Subject)
		if subject == "" || len(subject) > 255 {
			httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		profile, _ := validated.CustomClaims.(*claims)
		if profile != nil && strings.TrimSpace(profile.Email) == "" && slices.Contains(strings.Fields(profile.Scope), "openid") {
			profile, err = a.userInfo(r.Context(), validated.RegisteredClaims.Issuer, validated.RegisteredClaims.Subject, r.Header.Get("Authorization"))
			if err != nil {
				if errors.Is(err, errProfileUnauthorized) {
					w.Header().Set("WWW-Authenticate", "Bearer")
					httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				} else {
					httpx.JSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Account details are temporarily unavailable. Please try again."})
				}
				return
			}
		}
		var email *string
		displayName := ""
		if profile != nil {
			profile.Email = strings.TrimSpace(profile.Email)
			profile.Name = strings.TrimSpace(profile.Name)
			if profile.Email != "" && len(profile.Email) <= 320 {
				email = &profile.Email
			}
			if len(profile.Name) <= 200 {
				displayName = profile.Name
			}
		}
		user, err := a.store.GetOrCreate(r.Context(), subject, email, profile != nil && profile.EmailVerified, displayName)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
	}))
}
