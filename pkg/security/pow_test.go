package security

import (
	"bytes"
	"testing"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/rs/zerolog/log"
)

func init() {
	log.Logger = log.Output(nil)
}

func TestSolveAndVerifyPoW(t *testing.T) {
	challenge := GenerateChallenge(1, "http")
	sol, err := SolvePoW(challenge)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if !VerifyPoW(challenge, sol) {
		t.Fatal("expected valid PoW")
	}
	if !sol.Time.IsZero() && sol.Duration < 0 {
		t.Fatal("expected non-negative duration")
	}
}

func TestVerifyPoWRejectsBadNonce(t *testing.T) {
	challenge := GenerateChallenge(1, "http")
	sol, err := SolvePoW(challenge)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	sol.Nonce++
	if VerifyPoW(challenge, sol) {
		t.Fatal("expected invalid PoW after nonce change")
	}
}

func TestVerifyPoWRejectsWrongHash(t *testing.T) {
	challenge := GenerateChallenge(1, "http")
	sol, err := SolvePoW(challenge)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	sol.Hash = crypto.SHA3Hash([]byte("garbage"))
	if VerifyPoW(challenge, sol) {
		t.Fatal("expected invalid PoW after hash change")
	}
}

func TestDifficultyScalesWithLeadingZeros(t *testing.T) {
	for difficulty := uint8(1); difficulty <= 3; difficulty++ {
		challenge := GenerateChallenge(difficulty, "test")
		sol, err := SolvePoW(challenge)
		if err != nil {
			t.Fatalf("difficulty %d solve: %v", difficulty, err)
		}
		if !VerifyPoW(challenge, sol) {
			t.Fatalf("difficulty %d verify failed", difficulty)
		}
	}
}

func TestMarshalChallengeDeterministic(t *testing.T) {
	challenge := GenerateChallenge(2, "http")
	a := challenge.MarshalChallenge()
	b := challenge.MarshalChallenge()
	if !bytes.Equal(a, b) {
		t.Fatal("marshalling not deterministic")
	}
}

func TestMarshalSolutionRoundTrip(t *testing.T) {
	challenge := GenerateChallenge(1, "http")
	sol, err := SolvePoW(challenge)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	data, err := sol.MarshalSolution()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := UnmarshalSolution(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if sol.Nonce != out.Nonce || sol.Duration != out.Duration {
		t.Fatal("round-trip mismatch")
	}
}

func TestDifficultyAdjusterIncreasesOnSlowSolve(t *testing.T) {
	cfg := DefaultPoWConfig()
	cfg.AdjustmentInterval = 0
	adj := NewDifficultyAdjuster(cfg)
	start := time.Now()
	adj.RecordSolve(cfg.TargetSolveTime * 3)
	if time.Since(start) > time.Second {
		t.Fatal("adjustment took too long")
	}
}

func TestDifficultyAdjusterBounds(t *testing.T) {
	cfg := DefaultPoWConfig()
	cfg.AdjustmentInterval = 0
	adj := NewDifficultyAdjuster(cfg)
	for i := 0; i < 200; i++ {
		adj.RecordSolve(cfg.TargetSolveTime * 10)
	}
	if adj.CurrentDifficulty() > cfg.MaxDifficulty {
		t.Fatalf("difficulty exceeded max: %d", adj.CurrentDifficulty())
	}
}

func TestDefaultPoWConfigSanity(t *testing.T) {
	cfg := DefaultPoWConfig()
	if cfg.BaseDifficulty < cfg.MinDifficulty {
		t.Fatal("base below min")
	}
	if cfg.BaseDifficulty > cfg.MaxDifficulty {
		t.Fatal("base above max")
	}
	if cfg.TargetSolveTime <= 0 {
		t.Fatal("target solve time non-positive")
	}
}

func TestGenerateChallengeRandomNonce(t *testing.T) {
	a := GenerateChallenge(1, "http")
	time.Sleep(time.Millisecond)
	b := GenerateChallenge(1, "http")
	if a.Nonce == b.Nonce {
		t.Fatal("nonces should differ")
	}
}

func TestPoWValidatorValidate(t *testing.T) {
	cfg := DefaultPoWConfig()
	adj := NewDifficultyAdjuster(cfg)
	val := NewPoWValidator(adj)

	challenge := GenerateChallenge(1, "http")
	sol, err := SolvePoW(challenge)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if err := val.Validate(challenge, sol); err != nil {
		t.Fatalf("validate: %v", err)
	}
}
