package keycloak

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"
)

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwksResponse struct {
	Keys []jwk `json:"keys"`
}

type keySet struct {
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

type KeyFetcher struct {
	jwksURL    string
	httpClient *http.Client
	minRefresh time.Duration
	mu         sync.Mutex
	current    *keySet
}

func NewKeyFetcher(jwksURL string, httpClient *http.Client) *KeyFetcher {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &KeyFetcher{
		jwksURL:    jwksURL,
		httpClient: httpClient,
		minRefresh: 30 * time.Second,
	}
}

func (f *KeyFetcher) Key(kid string) (*rsa.PublicKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.current != nil {
		if key, ok := f.current.keys[kid]; ok {
			return key, nil
		}
		if time.Since(f.current.fetchedAt) < f.minRefresh {
			return nil, fmt.Errorf("keycloak: unknown key id %q (last refreshed %s ago)", kid, time.Since(f.current.fetchedAt))
		}
	}

	set, err := f.fetch()
	if err != nil {
		return nil, err
	}
	f.current = set

	key, ok := set.keys[kid]
	if !ok {
		return nil, fmt.Errorf("keycloak: unknown key id %q after refresh", kid)
	}
	return key, nil
}

func (f *KeyFetcher) fetch() (*keySet, error) {
	resp, err := f.httpClient.Get(f.jwksURL)
	if err != nil {
		return nil, fmt.Errorf("keycloak: fetch jwks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("keycloak: fetch jwks: unexpected status %d", resp.StatusCode)
	}

	var parsed jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("keycloak: decode jwks: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(parsed.Keys))
	for _, k := range parsed.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		pub, err := rsaPublicKeyFromJWK(k)
		if err != nil {
			return nil, fmt.Errorf("keycloak: parse key %q: %w", k.Kid, err)
		}
		keys[k.Kid] = pub
	}
	return &keySet{keys: keys, fetchedAt: time.Now()}, nil
}

func rsaPublicKeyFromJWK(k jwk) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode exponent: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)
	if !e.IsInt64() {
		return nil, fmt.Errorf("exponent too large")
	}

	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}
