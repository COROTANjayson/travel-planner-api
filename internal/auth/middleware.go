package auth

import (
	"context"
	"net/http"
	"strings"

	jwtmiddleware "github.com/auth0/go-jwt-middleware/v3"
	"github.com/auth0/go-jwt-middleware/v3/validator"
	"travel-planner/travel-planner-api/internal/httpx"
)

type claims struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (*claims) Validate(context.Context) error { return nil }

type userKey struct{}

func CurrentUser(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(userKey{}).(User)
	return user, ok
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
		user, err := a.store.GetOrCreate(r.Context(), subject, email, displayName)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, user)))
	}))
}
