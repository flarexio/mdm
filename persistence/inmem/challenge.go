package inmem

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"

	"github.com/flarexio/mdm-server/challenge"
)

// NewChallengeStore returns an in-memory challenge.Store whose challenges expire
// after ttl. It is fine for a single instance; a horizontally scaled deployment
// swaps in a shared backend (e.g. Redis) behind the same interface.
func NewChallengeStore(ttl time.Duration) (challenge.Store, error) {
	return &challengeStore{
		ttl:     ttl,
		entries: make(map[string]challengeEntry),
	}, nil
}

type challengeStore struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]challengeEntry
}

type challengeEntry struct {
	subject   string
	expiresAt time.Time
}

func (s *challengeStore) Generate(subject string) (string, error) {
	pw, err := randomChallenge()
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	s.entries[pw] = challengeEntry{
		subject:   subject,
		expiresAt: time.Now().Add(s.ttl),
	}
	s.mu.Unlock()

	return pw, nil
}

func (s *challengeStore) Redeem(pw string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[pw]
	if !ok {
		return "", challenge.ErrChallengeNotFound
	}

	// Consume the challenge regardless of outcome: even an expired one must not be
	// retryable. This is what makes it single-use.
	delete(s.entries, pw)

	if time.Now().After(e.expiresAt) {
		return "", challenge.ErrChallengeExpired
	}

	return e.subject, nil
}

// randomChallenge returns a cryptographically random, URL-safe challenge password.
func randomChallenge() (string, error) {
	b := make([]byte, 24) // 192 bits of entropy
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
