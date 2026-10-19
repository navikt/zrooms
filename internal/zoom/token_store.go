package zoom

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/navikt/zrooms/internal/repository"
)

// TokenBackend stores opaque OAuth records using compare-and-swap writes.
type TokenBackend interface {
	Read(context.Context, string) ([]byte, int64, error)
	Write(context.Context, string, []byte, int64) error
}

type authorization struct {
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	AuthorizedAt time.Time `json:"authorized_at,omitempty"`
	RevokedAt    time.Time `json:"revoked_at,omitempty"`
	Refreshing   bool      `json:"refreshing,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TokenStore encrypts OAuth records separately from meeting state.
type TokenStore struct {
	backend TokenBackend
	secret  string
}

// NewTokenStore derives a purpose-specific encryption key from the existing client secret.
func NewTokenStore(backend TokenBackend, clientSecret string) *TokenStore {
	return &TokenStore{backend: backend, secret: clientSecret}
}

func (s *TokenStore) aead() (cipher.AEAD, error) {
	if s.secret == "" {
		return nil, errors.New("OAuth token encryption is not configured")
	}
	key := sha256.Sum256([]byte("zrooms/oauth-tokens/v1\x00" + s.secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (s *TokenStore) read(ctx context.Context, userID string) (authorization, int64, error) {
	data, version, err := s.backend.Read(ctx, userID)
	if errors.Is(err, repository.ErrNotFound) {
		return authorization{}, 0, nil
	}
	if err != nil {
		return authorization{}, 0, err
	}
	aead, err := s.aead()
	if err != nil {
		return authorization{}, 0, err
	}
	if len(data) < aead.NonceSize() {
		return authorization{}, 0, errors.New("invalid encrypted OAuth record")
	}
	plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte(userID))
	if err != nil {
		return authorization{}, 0, errors.New("cannot decrypt OAuth record; reauthorization or key recovery is required")
	}
	var record authorization
	if json.Unmarshal(plain, &record) != nil || record.UpdatedAt.IsZero() {
		return authorization{}, 0, errors.New("invalid OAuth record")
	}
	// Match the existing bucket's seven-day lifecycle, including local development.
	if !time.Now().Before(record.UpdatedAt.Add(repository.Retention)) {
		return authorization{}, version, nil
	}
	return record, version, nil
}

func (s *TokenStore) write(ctx context.Context, userID string, record authorization, version int64) error {
	aead, err := s.aead()
	if err != nil {
		return err
	}
	record.UpdatedAt = time.Now().UTC()
	plain, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode OAuth record: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("generate OAuth encryption nonce: %w", err)
	}
	data := aead.Seal(nonce, nonce, plain, []byte(userID))
	return s.backend.Write(ctx, userID, data, version)
}

type memoryTokenEntry struct {
	data    []byte
	version int64
}

type memoryTokenBackend struct {
	mu       sync.Mutex
	entries  map[string]memoryTokenEntry
	revision int64
}

// NewMemoryTokenBackend keeps encrypted tokens in volatile local storage.
func NewMemoryTokenBackend() TokenBackend {
	return &memoryTokenBackend{entries: make(map[string]memoryTokenEntry)}
}

func (b *memoryTokenBackend) Read(ctx context.Context, id string) ([]byte, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	value, ok := b.entries[id]
	if !ok {
		return nil, 0, repository.ErrNotFound
	}
	return append([]byte(nil), value.data...), value.version, nil
}

func (b *memoryTokenBackend) Write(ctx context.Context, id string, data []byte, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.entries[id].version != version {
		return repository.ErrConflict
	}
	b.revision++
	b.entries[id] = memoryTokenEntry{data: append([]byte(nil), data...), version: b.revision}
	return nil
}
