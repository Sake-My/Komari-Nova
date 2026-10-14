package passkey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

const (
	ChallengeTTL      = 5 * time.Minute
	MaxChallenges     = 1024
	MaxRateEntries    = 4096
	RequestsPerMinute = 30
)

var ErrUnavailable = errors.New("passkey challenge expired, invalid, or already used")

type Challenge struct {
	Operation    string
	BrowserHash  [32]byte
	SessionHash  [32]byte
	UserUUID     string
	AccountStamp [32]byte
	Origin       string
	Name         string
	Data         webauthn.SessionData
	Expires      time.Time
}

type rateEntry struct {
	Window time.Time
	Count  int
}

type Store struct {
	mu         sync.Mutex
	challenges map[string]Challenge
	rates      map[string]rateEntry
}

func NewStore() *Store {
	return &Store{challenges: make(map[string]Challenge), rates: make(map[string]rateEntry)}
}

func RandomToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}

func TokenHash(value string) [32]byte { return sha256.Sum256([]byte(value)) }

func (s *Store) Allow(key string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, entry := range s.rates {
		if now.Sub(entry.Window) >= time.Minute {
			delete(s.rates, k)
		}
	}
	entry, exists := s.rates[key]
	if !exists {
		if len(s.rates) >= MaxRateEntries {
			return false
		}
		entry.Window = now
	}
	if entry.Count >= RequestsPerMinute {
		return false
	}
	entry.Count++
	s.rates[key] = entry
	return true
}

func (s *Store) Put(challenge Challenge, now time.Time) (string, error) {
	id, err := RandomToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, value := range s.challenges {
		if !value.Expires.After(now) {
			delete(s.challenges, key)
		}
	}
	if len(s.challenges) >= MaxChallenges {
		return "", errors.New("too many pending passkey challenges")
	}
	challenge.Expires = now.Add(ChallengeTTL)
	s.challenges[id] = challenge
	return id, nil
}

// Take 在校验绑定关系前消费挑战，失败的 finish 请求也不能被重放。
func (s *Store) Take(id, operation, browser, session, userUUID, origin string, now time.Time) (Challenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	challenge, ok := s.challenges[id]
	delete(s.challenges, id)
	if !ok || !challenge.Expires.After(now) || challenge.Operation != operation || browser == "" || challenge.BrowserHash != TokenHash(browser) || challenge.SessionHash != TokenHash(session) || challenge.UserUUID != userUUID || challenge.Origin != origin {
		return Challenge{}, ErrUnavailable
	}
	return challenge, nil
}
