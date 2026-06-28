package auth

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWKSVerifier verifies EdDSA tokens against a remote JWKS (identity's
// /.well-known/jwks.json). Keys are cached and refreshed on a TTL, or eagerly
// whenever a token references a key the cache does not hold — so a rotated key is
// picked up without a restart.
type JWKSVerifier struct {
	url      string
	issuer   string
	audience string
	http     *http.Client
	ttl      time.Duration

	mu      sync.RWMutex
	byKID   map[string]ed25519.PublicKey
	only    ed25519.PublicKey // used when a token carries no kid and there is exactly one key
	fetched time.Time
}

func NewJWKSVerifier(url, issuer, audience string, client *http.Client) *JWKSVerifier {
	if client == nil {
		client = http.DefaultClient
	}

	return &JWKSVerifier{
		url:      url,
		issuer:   issuer,
		audience: audience,
		http:     client,
		ttl:      1 * time.Hour,
	}
}

func (v *JWKSVerifier) Verify(ctx context.Context, bearer string) (*Claims, error) {
	if bearer == "" {
		return nil, ErrNoToken
	}

	opts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"EdDSA"}),
		jwt.WithLeeway(10 * time.Second),
	}
	if v.issuer != "" {
		opts = append(opts, jwt.WithIssuer(v.issuer))
	}
	if v.audience != "" {
		opts = append(opts, jwt.WithAudience(v.audience))
	}

	var claims Claims
	if _, err := jwt.ParseWithClaims(bearer, &claims, v.keyfunc(ctx), opts...); err != nil {
		return nil, err
	}

	return &claims, nil
}

// keyfunc resolves the verification key for a token. identity does not set a kid
// header, so a token usually arrives without one: that is unambiguous only while
// the JWKS holds a single key. A cache miss triggers one refresh before giving up.
func (v *JWKSVerifier) keyfunc(ctx context.Context) jwt.Keyfunc {
	return func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)

		if key, ok := v.lookup(kid); ok {
			return key, nil
		}

		if err := v.refresh(ctx); err != nil {
			return nil, err
		}

		if key, ok := v.lookup(kid); ok {
			return key, nil
		}

		return nil, errors.New("auth: no matching JWKS key")
	}
}

func (v *JWKSVerifier) lookup(kid string) (ed25519.PublicKey, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.fetched.IsZero() || time.Since(v.fetched) > v.ttl {
		return nil, false // stale (or never fetched): force a refresh
	}

	if kid != "" {
		key, ok := v.byKID[kid]
		return key, ok
	}

	return v.only, v.only != nil
}

type jwk struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Kid string `json:"kid"`
}

func (v *JWKSVerifier) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.url, nil)
	if err != nil {
		return err
	}

	resp, err := v.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth: jwks status %d", resp.StatusCode)
	}

	var set struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return err
	}

	byKID := make(map[string]ed25519.PublicKey)
	var usable []ed25519.PublicKey
	for _, k := range set.Keys {
		if k.Kty != "OKP" || k.Crv != "Ed25519" {
			continue
		}

		raw, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			continue
		}

		pub := ed25519.PublicKey(raw)
		usable = append(usable, pub)
		if k.Kid != "" {
			byKID[k.Kid] = pub
		}
	}

	if len(usable) == 0 {
		return errors.New("auth: jwks has no usable Ed25519 keys")
	}

	v.mu.Lock()
	v.byKID = byKID
	if len(usable) == 1 {
		v.only = usable[0]
	} else {
		v.only = nil
	}
	v.fetched = time.Now()
	v.mu.Unlock()

	return nil
}
