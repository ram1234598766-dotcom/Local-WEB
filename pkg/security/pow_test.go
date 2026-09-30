package security

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/rs/zerolog/log"
)

func init() {
	log.Logger = log.Output(nil)
}

func TestSolveAndVerifyPoW(t *testing.T) {
	challenge := GenerateChallenge(MinDifficulty, "http")
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

func TestVerifyPoWRejectsBadHash(t *testing.T) {
	challenge := GenerateChallenge(MinDifficulty, "http")
	sol, err := SolvePoW(challenge)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	sol.Hash = crypto.SHA3Hash([]byte("garbage"))
	if VerifyPoW(challenge, sol) {
		t.Fatal("expected invalid PoW after hash change")
	}
}

func TestVerifyPoWRejectsWrongChallenge(t *testing.T) {
	challenge := GenerateChallenge(MinDifficulty, "http")
	challenge2 := GenerateChallenge(MinDifficulty, "dns")
	sol, err := SolvePoW(challenge)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if VerifyPoW(challenge2, sol) {
		t.Fatal("expected invalid PoW for different challenge")
	}
}

func TestMarshalChallengeDeterministic(t *testing.T) {
	challenge := GenerateChallenge(16, "http")
	a := challenge.MarshalChallenge()
	b := challenge.MarshalChallenge()
	if !bytes.Equal(a, b) {
		t.Fatal("marshalling not deterministic")
	}
}

// The marshalled challenge is hashed by both solver and verifier, so any
// field in it must change the output. This guards the encoding against
// accidentally dropping a field from the hash pre-image.
func TestMarshalChallengeCoversEveryField(t *testing.T) {
	base := GenerateChallenge(16, "http")
	original := base.MarshalChallenge()

	variants := map[string]func(c *PoWChallenge){
		"difficulty":  func(c *PoWChallenge) { c.Difficulty++ },
		"memory":      func(c *PoWChallenge) { c.Memory++ },
		"time cost":   func(c *PoWChallenge) { c.TimeCost++ },
		"parallelism": func(c *PoWChallenge) { c.Parallelism++ },
		"timestamp":   func(c *PoWChallenge) { c.Timestamp = c.Timestamp.Add(time.Nanosecond) },
		"service":     func(c *PoWChallenge) { c.Service = "dns" },
		"salt":        func(c *PoWChallenge) { c.Salt[0] ^= 0xff },
	}
	for name, mutate := range variants {
		v := base
		mutate(&v)
		if bytes.Equal(v.MarshalChallenge(), original) {
			t.Errorf("marshalling ignores change to %s", name)
		}
	}
}

func TestMarshalSolutionRoundTrip(t *testing.T) {
	challenge := GenerateChallenge(MinDifficulty, "http")
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
	if sol.Duration != out.Duration {
		t.Fatal("round-trip duration mismatch")
	}
	if sol.Nonce != out.Nonce {
		t.Fatal("round-trip nonce mismatch")
	}
	if sol.Hash != out.Hash {
		t.Fatal("round-trip hash mismatch")
	}
	// A solution that survives the wire must still verify after a round trip.
	if !VerifyPoW(challenge, out) {
		t.Fatal("round-tripped solution failed verification")
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
	if cfg.Memory > MaxPoWMemoryKiB {
		t.Fatal("default memory exceeds hard cap")
	}
	if cfg.TimeCost > MaxPoWTimeCost {
		t.Fatal("default time cost exceeds hard cap")
	}
}

func TestGenerateChallengeRandomSalt(t *testing.T) {
	a := GenerateChallenge(MinDifficulty, "http")
	time.Sleep(time.Millisecond)
	b := GenerateChallenge(1, "http")
	if a.Salt == b.Salt {
		t.Fatal("salts should differ")
	}
}

func TestPoWValidatorValidate(t *testing.T) {
	cfg := DefaultPoWConfig()
	adj := NewDifficultyAdjuster(cfg)
	val := NewPoWValidator(adj)

	challenge := GenerateChallenge(MinDifficulty, "http")
	sol, err := SolvePoW(challenge)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if err := val.Validate(challenge, sol); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestPoWValidatorRejectsForgedSolution(t *testing.T) {
	val := NewPoWValidator(NewDifficultyAdjuster(DefaultPoWConfig()))
	challenge := GenerateChallenge(MinDifficulty, "http")
	sol, err := SolvePoW(challenge)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	sol.Hash = crypto.SHA3Hash([]byte("forged"))
	if err := val.Validate(challenge, sol); err == nil {
		t.Fatal("expected forged solution to be rejected")
	}
}

func TestArgon2idParametersInChallenge(t *testing.T) {
	challenge := GenerateChallenge(MinDifficulty, "http")
	if challenge.Algorithm != "argon2id-sha3" {
		t.Fatalf("expected argon2id-sha3 algorithm, got %s", challenge.Algorithm)
	}
	if challenge.Memory != 64*1024 {
		t.Fatalf("expected 64 MiB memory, got %d KiB", challenge.Memory)
	}
	// One Argon2id lane maximises memory-hardness per allocated MiB. The
	// memory-hard pass runs once per challenge, so there is nothing to gain
	// from extra lanes.
	if challenge.Parallelism != 1 {
		t.Fatalf("expected parallelism 1, got %d", challenge.Parallelism)
	}
	if challenge.TimeCost != 1 {
		t.Fatalf("expected time cost 1, got %d", challenge.TimeCost)
	}
}

func TestChallengeDifficultyIsClampedToSupportedRange(t *testing.T) {
	if got := GenerateChallenge(0, "http").Difficulty; got != MinDifficulty {
		t.Fatalf("difficulty 0 should clamp up to %d, got %d", MinDifficulty, got)
	}
	if got := GenerateChallenge(200, "http").Difficulty; got != MaxDifficulty {
		t.Fatalf("difficulty 200 should clamp down to %d, got %d", MaxDifficulty, got)
	}
}

func TestSolvePoWMeetsAdvertisedDifficulty(t *testing.T) {
	for _, difficulty := range []uint8{MinDifficulty, 16, 20} {
		challenge := GenerateChallenge(difficulty, "test")
		sol, err := SolvePoW(challenge)
		if err != nil {
			t.Fatalf("difficulty %d solve: %v", difficulty, err)
		}
		if !VerifyPoW(challenge, sol) {
			t.Fatalf("difficulty %d: solution did not verify", difficulty)
		}
		if got := leadingZeroBits(sol.Hash); got < int(difficulty) {
			t.Fatalf("difficulty %d: solution has only %d leading zero bits", difficulty, got)
		}
	}
}

// cheapChallenge builds a challenge with a minimal Argon2id memory cost and a
// caller-supplied salt. Cache and encoding behaviour is independent of the KDF
// cost, so bulk tests use this to stay in the sub-second range instead of
// paying 64 MiB per solve. The salt must vary per call: the replay cache keys
// on the whole marshalled challenge, and Windows clock resolution can make two
// time.Now() calls land on the same nanosecond.
func cheapChallenge(difficulty uint8, saltSeed int) PoWChallenge {
	var salt [16]byte
	digest := crypto.SHA3Hash([]byte{byte(difficulty), byte(saltSeed), byte(saltSeed >> 8), byte(saltSeed >> 16)})
	copy(salt[:], digest[:16])
	return PoWChallenge{
		Algorithm:   "argon2id-sha3",
		Difficulty:  difficulty,
		Memory:      8, // Argon2id minimum for 1 lane
		TimeCost:    1,
		Parallelism: 1,
		Timestamp:   time.Now(),
		Service:     "http",
		Salt:        salt,
	}
}

func TestSolvePoWIsBoundedInTime(t *testing.T) {
	// The previous implementation ran Argon2id (64 MiB, 2^difficulty
	// iterations) once per nonce, so difficulty 3 was unreachable and the
	// suite timed out after 600s. Assert the search is now tractable at a
	// realistic difficulty.
	challenge := GenerateChallenge(20, "test")
	start := time.Now()
	sol, err := SolvePoW(challenge)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	elapsed := time.Since(start)
	// 2^20 SHA3-256 hashes plus one 64 MiB Argon2id pass.
	if elapsed > 60*time.Second {
		t.Fatalf("difficulty 20 took %v, which is not tractable", elapsed)
	}
	if !VerifyPoW(challenge, sol) {
		t.Fatal("solution did not verify")
	}
	t.Logf("difficulty 20 solved in %v", elapsed)
}

// A difficulty the system will never issue must be refused outright rather
// than accepted and then searched for until the nonce space runs out.
func TestSolvePoWRefusesDifficultyAboveMaximum(t *testing.T) {
	challenge := GenerateChallenge(MinDifficulty, "test")
	challenge.Difficulty = MaxDifficulty + 1
	if _, err := SolvePoW(challenge); err == nil {
		t.Fatal("expected difficulty above maximum to be refused")
	}
}

func TestSolvePoWRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SolvePoWContext(ctx, GenerateChallenge(MaxDifficulty, "test")); err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestVerifyPoWRejectsStaleChallenge(t *testing.T) {
	challenge := GenerateChallenge(MinDifficulty, "http")
	sol, err := SolvePoW(challenge)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	stale := challenge
	stale.Timestamp = challenge.Timestamp.Add(-2 * PowReplayWindow)
	if VerifyPoWWithTime(stale, sol, time.Now()) {
		t.Fatal("expected stale challenge to be rejected by freshness check")
	}
	// The timestamp is part of the marshalled challenge and therefore part of
	// the work seed, so an aged challenge does not merely fail a clock check:
	// its solution no longer verifies at all. That is what stops a captured
	// solution from being replayed after the window it was minted in.
	if VerifyPoW(stale, sol) {
		t.Fatal("expected aged challenge to fail verification, not just the freshness check")
	}
}

func TestVerifyPoWRejectsTamperedSolutionFields(t *testing.T) {
	challenge := GenerateChallenge(MinDifficulty, "http")
	sol, err := SolvePoW(challenge)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	tamperedNonce := sol
	tamperedNonce.Nonce[0] ^= 0xff
	if VerifyPoW(challenge, tamperedNonce) {
		t.Fatal("expected tampered nonce to be rejected")
	}
	tamperedHash := sol
	tamperedHash.Hash[31] ^= 0x01
	if VerifyPoW(challenge, tamperedHash) {
		t.Fatal("expected tampered hash to be rejected")
	}
}

// A challenge is attacker-controlled data. Its declared cost must never be
// able to make the verifier allocate more than the hard caps allow.
func TestChallengeCostIsClampedAgainstDoS(t *testing.T) {
	hostile := PoWChallenge{
		Algorithm:   "argon2id-sha3",
		Difficulty:  MinDifficulty,
		Memory:      1 << 30, // 1 TiB requested
		TimeCost:    1 << 20,
		Parallelism: 255,
		Timestamp:   time.Now(),
		Service:     "http",
	}
	memory, timeCost, parallelism := hostile.clamp()
	if memory != MaxPoWMemoryKiB {
		t.Fatalf("memory not clamped: %d", memory)
	}
	if timeCost != MaxPoWTimeCost {
		t.Fatalf("time cost not clamped: %d", timeCost)
	}
	if parallelism != MaxPoWParallelism {
		t.Fatalf("parallelism not clamped: %d", parallelism)
	}
	if memory < 8*uint32(parallelism) {
		t.Fatalf("clamped memory %d below Argon2id minimum for %d lanes", memory, parallelism)
	}
}

func TestPoWValidatorRejectsReplay(t *testing.T) {
	val := NewPoWValidator(NewDifficultyAdjuster(DefaultPoWConfig()))
	challenge := GenerateChallenge(MinDifficulty, "http")
	sol, err := SolvePoW(challenge)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if err := val.Validate(challenge, sol); err != nil {
		t.Fatalf("first validate: %v", err)
	}
	if err := val.Validate(challenge, sol); err == nil {
		t.Fatal("expected replay of the same solution to be rejected")
	}
}

func TestPoWValidatorReplayCacheStaysBounded(t *testing.T) {
	val := NewPoWValidator(NewDifficultyAdjuster(DefaultPoWConfig()))
	// Every challenge is distinct, so all are accepted; the cache must not
	// grow without bound. Cheap challenges keep the loop fast.
	for i := 0; i < 1500; i++ {
		challenge := cheapChallenge(MinDifficulty, i)
		sol, err := SolvePoW(challenge)
		if err != nil {
			t.Fatalf("solve %d: %v", i, err)
		}
		if err := val.Validate(challenge, sol); err != nil {
			t.Fatalf("validate %d: %v", i, err)
		}
	}
	val.mu.RLock()
	size := len(val.seen)
	val.mu.RUnlock()
	if size > 1024 {
		t.Fatalf("replay cache grew to %d entries, expected <= 1024", size)
	}
}

func TestLeadingZeroBitsCountsCorrectly(t *testing.T) {
	cases := []struct {
		hash [32]byte
		want int
	}{
		{[32]byte{}, 256},
		{[32]byte{0x80}, 0},
		{[32]byte{0x40}, 1},
		{[32]byte{0x01}, 7},
		{[32]byte{0x00, 0x00, 0x00, 0xff}, 24},
	}
	for i, tc := range cases {
		if got := leadingZeroBits(tc.hash); got != tc.want {
			t.Errorf("case %d: got %d leading zero bits, want %d", i, got, tc.want)
		}
	}
}

// Solve time is exponential in difficulty, so an average slower than target
// means we are asking for too much work and difficulty must come down.
func TestDifficultyAdjusterLowersDifficultyOnSlowSolve(t *testing.T) {
	cfg := DefaultPoWConfig()
	cfg.AdjustmentInterval = 0
	adj := NewDifficultyAdjuster(cfg)
	start := adj.CurrentDifficulty()
	for i := 0; i < 10; i++ {
		adj.RecordSolve(cfg.TargetSolveTime * 4)
	}
	if adj.CurrentDifficulty() >= start {
		t.Fatalf("difficulty should fall when solves are slow: %d -> %d",
			start, adj.CurrentDifficulty())
	}
}

func TestDifficultyAdjusterRaisesDifficultyOnFastSolve(t *testing.T) {
	cfg := DefaultPoWConfig()
	cfg.AdjustmentInterval = 0
	adj := NewDifficultyAdjuster(cfg)
	start := adj.CurrentDifficulty()
	for i := 0; i < 10; i++ {
		adj.RecordSolve(cfg.TargetSolveTime / 4)
	}
	if adj.CurrentDifficulty() <= start {
		t.Fatalf("difficulty should rise when solves are fast: %d -> %d",
			start, adj.CurrentDifficulty())
	}
}

// The correction is bounded, so a single outlier sample cannot collapse the
// difficulty and make the subsystem trivially cheap to attack.
func TestDifficultyAdjusterBoundsSingleStep(t *testing.T) {
	cfg := DefaultPoWConfig()
	cfg.AdjustmentInterval = 0
	adj := NewDifficultyAdjuster(cfg)
	start := adj.CurrentDifficulty()
	adj.RecordSolve(cfg.TargetSolveTime * 1000000)
	drop := start - adj.CurrentDifficulty()
	// MaxAdjustmentFactor 2.0 caps the step at log2(2) = 1 bit.
	if drop > 1 {
		t.Fatalf("single adjustment moved difficulty by %d bits, want at most 1", drop)
	}
}

func TestDifficultyAdjusterHonoursMinBound(t *testing.T) {
	cfg := DefaultPoWConfig()
	cfg.AdjustmentInterval = 0
	adj := NewDifficultyAdjuster(cfg)
	for i := 0; i < 500; i++ {
		adj.RecordSolve(time.Nanosecond)
	}
	if adj.CurrentDifficulty() < cfg.MinDifficulty {
		t.Fatalf("difficulty %d below min %d", adj.CurrentDifficulty(), cfg.MinDifficulty)
	}
}
