package email

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
)

// NewPoWChecker creates a new proof-of-work checker.
func NewPoWChecker(difficulty int) *PoWChecker {
	return &PoWChecker{
		difficulty: difficulty,
		nonces:     make(map[string]time.Time),
	}
}

// VerifyPoW verifies a proof-of-work solution.
// The format is: sha3(email + nonce) must have at least `difficulty` leading zero bits.
func (p *PoWChecker) VerifyPoW(email, nonce string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if t, ok := p.nonces[nonce]; ok {
		if time.Since(t) < 5*time.Minute {
			return false
		}
		delete(p.nonces, nonce)
	}

	data := email + nonce
	hash := crypto.SHA3Hash([]byte(data))

	leadingZeros := 0
	for _, b := range hash[:] {
		if b == 0 {
			leadingZeros += 8
		} else {
			for i := 7; i >= 0; i-- {
				if (b>>i)&1 == 0 {
					leadingZeros++
				} else {
					break
				}
			}
			break
		}
	}

	if leadingZeros >= p.difficulty {
		p.nonces[nonce] = time.Now()
		return true
	}
	return false
}

// GeneratePoW generates a valid nonce for the given email.
func (p *PoWChecker) GeneratePoW(email string) (string, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}

	for i := uint64(0); ; i++ {
		candidate := fmt.Sprintf("%s-%d", hex.EncodeToString(nonce[:]), i)
		data := email + candidate
		hash := crypto.SHA3Hash([]byte(data))

		leadingZeros := 0
		for _, b := range hash[:] {
			if b == 0 {
				leadingZeros += 8
			} else {
				for j := 7; j >= 0; j-- {
					if (b>>j)&1 == 0 {
						leadingZeros++
					} else {
						break
					}
				}
				break
			}
		}

		if leadingZeros >= p.difficulty {
			return candidate, nil
		}
	}
}

// Cleanup removes expired nonces.
func (p *PoWChecker) Cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for nonce, t := range p.nonces {
		if now.Sub(t) > 5*time.Minute {
			delete(p.nonces, nonce)
		}
	}
}

// Difficulty returns the current difficulty.
func (p *PoWChecker) Difficulty() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.difficulty
}

// SetDifficulty sets the difficulty.
func (p *PoWChecker) SetDifficulty(d int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.difficulty = d
}

// IsSpam checks if a message should be considered spam based on headers.
func IsSpam(msg *Message) bool {
	if msg == nil {
		return true
	}

	spamHeaders := []string{"X-Spam-Flag", "X-Spam-Status"}
	for _, h := range spamHeaders {
		if v, ok := msg.Headers[h]; ok {
			if strings.Contains(strings.ToLower(v), "yes") {
				return true
			}
		}
	}

	if score, ok := msg.Headers["X-Spam-Score"]; ok {
		var s float64
		if _, err := fmt.Sscanf(score, "%f", &s); err == nil && s > 5.0 {
			return true
		}
	}

	return false
}

// CheckRateLimit checks if a sender is rate-limited.
type RateLimiter struct {
	emails map[string][]time.Time
	limit  int
	window time.Duration
	mu     sync.RWMutex
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		emails: make(map[string][]time.Time),
		limit:  limit,
		window: window,
	}
}

// Allow checks if a sender is allowed to send.
func (r *RateLimiter) Allow(sender string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	var recent []time.Time
	for _, t := range r.emails[sender] {
		if now.Sub(t) <= r.window {
			recent = append(recent, t)
		}
	}

	if len(recent) >= r.limit {
		return false
	}

	recent = append(recent, now)
	r.emails[sender] = recent
	return true
}

// Reset resets the rate limiter for a sender.
func (r *RateLimiter) Reset(sender string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.emails, sender)
}
