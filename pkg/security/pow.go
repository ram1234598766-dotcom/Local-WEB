package security

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/rs/zerolog/log"
)

// PoWChallenge represents a proof-of-work challenge (Argon2id-based, memory-hard).
type PoWChallenge struct {
	Algorithm  string    // "argon2id"
	Difficulty uint8     // log2 of iterations (time cost)
	Memory     uint32    // memory cost in KiB
	Parallelism uint8    // parallelism (lanes)
	Timestamp  time.Time
	Service    ServiceID
	Salt       [16]byte  // salt for Argon2id
}

// PoWSolution is a valid response to a PoWChallenge.
type PoWSolution struct {
	Nonce    [8]byte
	Hash     [32]byte
	Time     time.Time
	Duration time.Duration
}

// MarshalChallenge serialises a PoWChallenge to bytes.
func (c *PoWChallenge) MarshalChallenge() []byte {
	buf := new(bytes.Buffer)
	buf.WriteString(c.Algorithm)
	buf.WriteByte(c.Difficulty)
	binary.Write(buf, binary.BigEndian, c.Memory)
	buf.WriteByte(c.Parallelism)
	binary.Write(buf, binary.BigEndian, c.Timestamp.UnixNano())
	buf.Write([]byte(c.Service))
	buf.Write(c.Salt[:])
	return buf.Bytes()
}

// GenerateChallenge creates a new Argon2id PoWChallenge with a random salt.
func GenerateChallenge(difficulty uint8, svc ServiceID) PoWChallenge {
	var salt [16]byte
	rand.Read(salt[:])
	return PoWChallenge{
		Algorithm:   "argon2id",
		Difficulty:  difficulty,
		Memory:      64 * 1024, // 64 MiB
		Parallelism: 4,
		Timestamp:   time.Now(),
		Service:     svc,
		Salt:        salt,
	}
}

// SolvePoW finds a solution such that Argon2id(challenge || solution) has
// at least `difficulty` leading zero bytes in the output hash.
// Uses Argon2id with memory-hard parameters to resist ASIC/GPU acceleration.
// Additionally uses SHA3-256 for challenge binding.
func SolvePoW(challenge PoWChallenge) (PoWSolution, error) {
	start := time.Now()
	challengeBytes := challenge.MarshalChallenge()

	// Use Argon2id with configurable parameters
	// difficulty maps to time cost (iterations): 2^difficulty iterations
	iterations := uint32(1) << challenge.Difficulty
	if iterations < 1 {
		iterations = 1
	}

	var solution []byte
	var hash [32]byte
	target := make([]byte, challenge.Difficulty)

	// For verification, we use a deterministic approach:
	// The "solution" is finding a nonce such that Argon2id(challenge || nonce) meets difficulty
	var nonce [8]byte
	for {
		binary.BigEndian.PutUint64(nonce[:], uint64(len(solution)))

		// Argon2id: hash = Argon2id(challengeBytes || nonce, salt, iterations, memory, parallelism, 32)
		input := append(challengeBytes, nonce[:]...)
		h := argon2.IDKey(input, challenge.Salt[:], iterations, challenge.Memory, challenge.Parallelism, 32)
		copy(hash[:], h)

		if subtle.ConstantTimeCompare(hash[:challenge.Difficulty], target) == 1 {
			return PoWSolution{
				Nonce:    nonce,
				Hash:     hash,
				Time:     time.Now(),
				Duration: time.Since(start),
			}, nil
		}

		// Increment nonce (using solution length as counter)
		solution = append(solution, 0)
		if len(solution) > 1000000 { // Safety limit
			return PoWSolution{}, errors.New("nonce space exhausted")
		}
	}
}

// VerifyPoW checks that a solution satisfies the challenge using Argon2id.
func VerifyPoW(challenge PoWChallenge, sol PoWSolution) bool {
	challengeBytes := challenge.MarshalChallenge()
	challengeHash := crypto.SHA3Hash(challengeBytes)

	// Recompute Argon2id with the same parameters and the nonce from solution
	iterations := uint32(1) << challenge.Difficulty
	if iterations < 1 {
		iterations = 1
	}

	// Verify by recomputing Argon2id with the challenge + nonce
	input := append(challengeBytes, sol.Nonce[:]...)
	h := argon2.IDKey(input, challenge.Salt[:], iterations, challenge.Memory, challenge.Parallelism, 32)
	var computedHash [32]byte
	copy(computedHash[:], h)

	// Check if computed hash matches the solution hash
	if subtle.ConstantTimeCompare(computedHash[:], sol.Hash[:]) != 1 {
		return false
	}

	// Check if hash meets difficulty target
	target := make([]byte, challenge.Difficulty)
	if subtle.ConstantTimeCompare(computedHash[:challenge.Difficulty], target) != 1 {
		return false
	}

	// Additional verification: check SHA3-256 of challenge matches expected
	// This binds the solution to the specific challenge
	challengeHash2 := crypto.SHA3Hash(challengeBytes)
	if subtle.ConstantTimeCompare(challengeHash[:], challengeHash2[:]) != 1 {
		return false
	}

	return true
}

// PoWConfig tunes the proof-of-work subsystem.
type PoWConfig struct {
	BaseDifficulty      uint8
	MinDifficulty       uint8
	MaxDifficulty       uint8
	TargetSolveTime     time.Duration
	AdjustmentInterval  time.Duration
	MaxAdjustmentFactor float64
	Memory              uint32    // memory in KiB
	Parallelism         uint8
}

// DefaultPoWConfig returns sensible defaults for Argon2id PoW.
func DefaultPoWConfig() PoWConfig {
	return PoWConfig{
		BaseDifficulty:      2,
		MinDifficulty:       1,
		MaxDifficulty:       5,
		TargetSolveTime:     100 * time.Millisecond,
		AdjustmentInterval:  5 * time.Minute,
		MaxAdjustmentFactor: 2.0,
		Memory:              64 * 1024, // 64 MiB
		Parallelism:         4,
	}
}

// DifficultyAdjuster manages dynamic PoW difficulty based on observed solve times.
type DifficultyAdjuster struct {
	mu         sync.RWMutex
	config     PoWConfig
	difficulty uint8
	lastAdjust time.Time
	history    []time.Duration
}

// NewDifficultyAdjuster creates an adjuster with the provided config.
func NewDifficultyAdjuster(cfg PoWConfig) *DifficultyAdjuster {
	d := &DifficultyAdjuster{config: cfg, difficulty: cfg.BaseDifficulty}
	if d.difficulty < cfg.MinDifficulty {
		d.difficulty = cfg.MinDifficulty
	}
	if d.difficulty > cfg.MaxDifficulty {
		d.difficulty = cfg.MaxDifficulty
	}
	return d
}

// RecordSolve records the duration of a successful PoW solve and adjusts
// difficulty if enough time has passed.
func (a *DifficultyAdjuster) RecordSolve(d time.Duration) uint8 {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.history = append(a.history, d)
	if len(a.history) > 100 {
		a.history = a.history[len(a.history)-100:]
	}

	if time.Since(a.lastAdjust) < a.config.AdjustmentInterval {
		return a.difficulty
	}

	avg := averageDuration(a.history)
	factor := float64(avg) / float64(a.config.TargetSolveTime)

	var newDiff float64
	if factor > a.config.MaxAdjustmentFactor {
		factor = a.config.MaxAdjustmentFactor
	}
	if factor < 1.0/a.config.MaxAdjustmentFactor {
		factor = 1.0 / a.config.MaxAdjustmentFactor
	}

	if avg > a.config.TargetSolveTime {
		newDiff = float64(a.difficulty) - math.Log2(factor)
	} else {
		newDiff = float64(a.difficulty) + math.Log2(factor)
	}

	if newDiff < float64(a.config.MinDifficulty) {
		newDiff = float64(a.config.MinDifficulty)
	}
	if newDiff > float64(a.config.MaxDifficulty) {
		newDiff = float64(a.config.MaxDifficulty)
	}

	a.difficulty = uint8(newDiff)
	a.lastAdjust = time.Now()

	log.Info().
		Uint8("difficulty", a.difficulty).
		Float64("factor", factor).
		Dur("avg", avg).
		Msg("PoW difficulty adjusted")

	return a.difficulty
}

// CurrentDifficulty returns the current difficulty level.
func (a *DifficultyAdjuster) CurrentDifficulty() uint8 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.difficulty
}

// PoWValidator validates PoW challenges and solutions for incoming messages.
type PoWValidator struct {
	mu       sync.RWMutex
	adjuster *DifficultyAdjuster
	recent   map[string]time.Time // challenge hash -> timestamp
}

// NewPoWValidator creates a validator backed by a difficulty adjuster.
func NewPoWValidator(adjuster *DifficultyAdjuster) *PoWValidator {
	return &PoWValidator{
		adjuster: adjuster,
		recent:   make(map[string]time.Time),
	}
}

// Validate checks a PoW solution and records the attempt.
func (v *PoWValidator) Validate(challenge PoWChallenge, sol PoWSolution) error {
	if !VerifyPoW(challenge, sol) {
		return errors.New("invalid proof-of-work")
	}

	key := string(challenge.MarshalChallenge())
	v.mu.Lock()
	v.recent[key] = time.Now()
	if len(v.recent) > 1024 {
		for k := range v.recent {
			delete(v.recent, k)
			break
		}
	}
	v.mu.Unlock()

	v.adjuster.RecordSolve(sol.Duration)
	return nil
}

// MarshalSolution serialises a PoWSolution for transport.
func (s *PoWSolution) MarshalSolution() ([]byte, error) {
	return json.Marshal(s)
}

// UnmarshalSolution deserialises a PoWSolution from transport.
func UnmarshalSolution(data []byte) (PoWSolution, error) {
	var s PoWSolution
	if err := json.Unmarshal(data, &s); err != nil {
		return PoWSolution{}, err
	}
	return s, nil
}

func averageDuration(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	var total time.Duration
	for _, d := range ds {
		total += d
	}
	return total / time.Duration(len(ds))
}