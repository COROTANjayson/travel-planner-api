package auth

import (
	"net/http"
	"net/url"

	jwtmiddleware "github.com/auth0/go-jwt-middleware/v3"
	"github.com/auth0/go-jwt-middleware/v3/jwks"
	"github.com/auth0/go-jwt-middleware/v3/validator"
	"travel-planner/travel-planner-api/internal/httpx"
)

type Auth struct {
	jwt   *jwtmiddleware.JWTMiddleware
	store Store
}

func New(issuer string, audience string, store Store) (*Auth, error) {
	issuerURL, err := url.Parse(issuer)
	if err != nil {
		return nil, err
	}
	provider, err := jwks.NewCachingProvider(jwks.WithIssuerURL(issuerURL))
	if err != nil {
		return nil, err
	}
	jwtValidator, err := validator.New(
		validator.WithKeyFunc(provider.KeyFunc),
		validator.WithAlgorithm(validator.RS256),
		validator.WithIssuer(issuer),
		validator.WithAudience(audience),
		validator.WithCustomClaims(func() *claims { return &claims{} }),
	)
	if err != nil {
		return nil, err
	}
	return newAuth(jwtValidator, store)
}

func newAuth(jwtValidator *validator.Validator, store Store) (*Auth, error) {
	middleware, err := jwtmiddleware.New(
		jwtmiddleware.WithValidator(jwtValidator),
		jwtmiddleware.WithErrorHandler(func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}),
	)
	if err != nil {
		return nil, err
	}
	return &Auth{jwt: middleware, store: store}, nil
}
