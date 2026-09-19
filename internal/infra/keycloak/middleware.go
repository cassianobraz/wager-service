package keycloak

import (
	"context"
	"errors"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"net/http"
	"strings"
)

type Config struct {
	Issuer        string
	JWKSURL       string
	Audience      string
	ProviderClaim string
}

func (c Config) providerClaim() string {
	if c.ProviderClaim == "" {
		return "azp"
	}
	return c.ProviderClaim
}

type Authenticator struct {
	cfg  Config
	keys *KeyFetcher
}

func NewAuthenticator(cfg Config, keys *KeyFetcher) *Authenticator {
	return &Authenticator{cfg: cfg, keys: keys}
}

type providerIDKey struct{}

func ProviderIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(providerIDKey{}).(string)
	return id, ok
}

func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenString, err := bearerToken(r)
		if err != nil {
			unauthorized(w, err.Error())
			return
		}

		providerID, err := a.authenticate(tokenString)
		if err != nil {
			unauthorized(w, err.Error())
			return
		}

		ctx := context.WithValue(r.Context(), providerIDKey{}, providerID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func bearerToken(r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", errors.New("missing Authorization header")
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", errors.New("Authorization header must use the Bearer scheme")
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return "", errors.New("empty bearer token")
	}
	return token, nil
}

func (a *Authenticator) authenticate(tokenString string) (string, error) {
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, a.keyFunc, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(a.cfg.Issuer))
	if err != nil {
		return "", fmt.Errorf("invalid token: %w", err)
	}

	if a.cfg.Audience != "" {
		aud, err := claims.GetAudience()
		if err != nil || !containsString(aud, a.cfg.Audience) {
			return "", fmt.Errorf("token audience does not include %q", a.cfg.Audience)
		}
	}

	providerID, _ := claims[a.cfg.providerClaim()].(string)
	if providerID == "" {
		return "", fmt.Errorf("token is missing required claim %q", a.cfg.providerClaim())
	}
	return providerID, nil
}

func (a *Authenticator) keyFunc(token *jwt.Token) (any, error) {
	if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
		return nil, fmt.Errorf("unexpected signing method %v", token.Header["alg"])
	}
	kid, ok := token.Header["kid"].(string)
	if !ok || kid == "" {
		return nil, errors.New("token header is missing kid")
	}
	return a.keys.Key(kid)
}

func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

func unauthorized(w http.ResponseWriter, reason string) {
	w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprintf(w, `{"error":"unauthorized","message":%q}`, reason)
}
