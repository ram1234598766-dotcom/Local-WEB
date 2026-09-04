package email

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoWChecker_GenerateAndVerify(t *testing.T) {
	checker := NewPoWChecker(4)
	email := "user@example.com"

	nonce, err := checker.GeneratePoW(email)
	require.NoError(t, err)
	assert.NotEmpty(t, nonce)

	assert.True(t, checker.VerifyPoW(email, nonce))
	assert.False(t, checker.VerifyPoW("other@example.com", nonce))
}

func TestPoWChecker_ReplayProtection(t *testing.T) {
	checker := NewPoWChecker(4)
	email := "user@example.com"

	nonce, err := checker.GeneratePoW(email)
	require.NoError(t, err)

	assert.True(t, checker.VerifyPoW(email, nonce))
	assert.False(t, checker.VerifyPoW(email, nonce))
}

func TestPoWChecker_Cleanup(t *testing.T) {
	checker := NewPoWChecker(4)
	checker.nonces["old"] = time.Now().Add(-10 * time.Minute)
	checker.nonces["new"] = time.Now()

	checker.Cleanup()

	_, existsOld := checker.nonces["old"]
	_, existsNew := checker.nonces["new"]
	assert.False(t, existsOld)
	assert.True(t, existsNew)
}

func TestPoWChecker_DifficultyAdjustment(t *testing.T) {
	checker := NewPoWChecker(4)
	assert.Equal(t, 4, checker.Difficulty())

	checker.SetDifficulty(8)
	assert.Equal(t, 8, checker.Difficulty())
}

func TestIsSpam(t *testing.T) {
	t.Run("nil message is spam", func(t *testing.T) {
		assert.True(t, IsSpam(nil))
	})

	t.Run("clean message is not spam", func(t *testing.T) {
		msg := &Message{
			From:    "user@example.com",
			To:      []string{"recipient@example.com"},
			Subject: "Hello",
			Body:    "Test body",
			Headers: map[string]string{},
		}
		assert.False(t, IsSpam(msg))
	})

	t.Run("X-Spam-Flag yes is spam", func(t *testing.T) {
		msg := &Message{
			From:    "user@example.com",
			To:      []string{"recipient@example.com"},
			Subject: "Hello",
			Body:    "Test body",
			Headers: map[string]string{"X-Spam-Flag": "YES"},
		}
		assert.True(t, IsSpam(msg))
	})

	t.Run("high spam score is spam", func(t *testing.T) {
		msg := &Message{
			From:    "user@example.com",
			To:      []string{"recipient@example.com"},
			Subject: "Hello",
			Body:    "Test body",
			Headers: map[string]string{"X-Spam-Score": "10.5"},
		}
		assert.True(t, IsSpam(msg))
	})

	t.Run("low spam score is not spam", func(t *testing.T) {
		msg := &Message{
			From:    "user@example.com",
			To:      []string{"recipient@example.com"},
			Subject: "Hello",
			Body:    "Test body",
			Headers: map[string]string{"X-Spam-Score": "2.5"},
		}
		assert.False(t, IsSpam(msg))
	})
}

func TestRateLimiter(t *testing.T) {
	t.Run("allows within limit", func(t *testing.T) {
		limiter := NewRateLimiter(3, time.Minute)
		assert.True(t, limiter.Allow("user1"))
		assert.True(t, limiter.Allow("user1"))
		assert.True(t, limiter.Allow("user1"))
	})

	t.Run("blocks over limit", func(t *testing.T) {
		limiter := NewRateLimiter(2, time.Minute)
		assert.True(t, limiter.Allow("user1"))
		assert.True(t, limiter.Allow("user1"))
		assert.False(t, limiter.Allow("user1"))
	})

	t.Run("separate users independent", func(t *testing.T) {
		limiter := NewRateLimiter(1, time.Minute)
		assert.True(t, limiter.Allow("user1"))
		assert.False(t, limiter.Allow("user1"))
		assert.True(t, limiter.Allow("user2"))
	})

	t.Run("reset works", func(t *testing.T) {
		limiter := NewRateLimiter(1, time.Minute)
		assert.True(t, limiter.Allow("user1"))
		assert.False(t, limiter.Allow("user1"))
		limiter.Reset("user1")
		assert.True(t, limiter.Allow("user1"))
	})
}

func TestPoWChecker_DifferentDifficulties(t *testing.T) {
	for _, diff := range []int{1, 4, 8, 12} {
		checker := NewPoWChecker(diff)
		email := "test@example.com"
		nonce, err := checker.GeneratePoW(email)
		require.NoError(t, err)
		assert.True(t, checker.VerifyPoW(email, nonce), "difficulty %d", diff)
	}
}

func TestRateLimiter_Window(t *testing.T) {
	limiter := NewRateLimiter(1, 10*time.Millisecond)
	assert.True(t, limiter.Allow("user1"))
	assert.False(t, limiter.Allow("user1"))

	time.Sleep(15 * time.Millisecond)
	assert.True(t, limiter.Allow("user1"))
}
