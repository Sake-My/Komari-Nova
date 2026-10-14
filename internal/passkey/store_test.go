package passkey

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestChallengeBindingExpiryAndSingleUse(t *testing.T) {
	now := time.Now()
	base := Challenge{Operation: "register", BrowserHash: TokenHash("browser"), SessionHash: TokenHash("session"), UserUUID: "user", Origin: "https://panel.example"}
	for _, mode := range []string{"valid", "browser", "session", "operation", "user", "origin", "expired"} {
		t.Run(mode, func(t *testing.T) {
			s := NewStore()
			id, err := s.Put(base, now)
			if err != nil {
				t.Fatal(err)
			}
			operation, browser, session, user, origin, at := "register", "browser", "session", "user", "https://panel.example", now
			switch mode {
			case "browser":
				browser = "other"
			case "session":
				session = "other"
			case "operation":
				operation = "login"
			case "user":
				user = "other"
			case "origin":
				origin = "https://other.example"
			case "expired":
				at = now.Add(ChallengeTTL)
			}
			_, err = s.Take(id, operation, browser, session, user, origin, at)
			if (err == nil) != (mode == "valid") {
				t.Fatalf("result: %v", err)
			}
			if _, err = s.Take(id, "register", "browser", "session", "user", "https://panel.example", now); err == nil {
				t.Fatal("challenge replay accepted")
			}
		})
	}
	s := NewStore()
	id, _ := s.Put(base, now)
	var successful atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Take(id, "register", "browser", "session", "user", "https://panel.example", now); err == nil {
				successful.Add(1)
			}
		}()
	}
	wg.Wait()
	if successful.Load() != 1 {
		t.Fatalf("atomic consumption successes = %d", successful.Load())
	}
}

func TestStoreBoundsAndRateLimit(t *testing.T) {
	s := NewStore()
	now := time.Now()
	for i := 0; i < RequestsPerMinute; i++ {
		if !s.Allow("ip", now) {
			t.Fatal("early rate limit")
		}
	}
	if s.Allow("ip", now) {
		t.Fatal("rate limit missing")
	}
	if !s.Allow("ip", now.Add(time.Minute)) {
		t.Fatal("rate limit did not expire")
	}
	for i := 0; i < MaxChallenges; i++ {
		if _, err := s.Put(Challenge{}, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Put(Challenge{}, now); err == nil {
		t.Fatal("challenge capacity exceeded")
	}
	if _, err := s.Put(Challenge{}, now.Add(ChallengeTTL)); err != nil {
		t.Fatal(err)
	}
	s = NewStore()
	for i := 0; i < MaxRateEntries; i++ {
		s.Allow(fmt.Sprint(i), now)
	}
	if s.Allow("overflow", now) {
		t.Fatal("limiter capacity exceeded")
	}
}
