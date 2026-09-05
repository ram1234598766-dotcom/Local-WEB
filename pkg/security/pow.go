package security

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/mrityunjay/LocalWEB/pkg/crypto"
	"github.com/rs/zerolog/log"
)

// PoWChallenge represents a proof-of-work challenge.
type PoWChallenge struct {
	Difficulty uint8
	Timestamp  time.Time
	Service    ServiceID
	Nonce      [8]byte
}

// PoWSolution is a valid response to a PoWChallenge.
type PoWSolution struct {
	Nonce    uint64
	Hash     [32]byte
	Time     time.Time
	Duration time.Duration
}

// MarshalChallenge serialises a PoWChallenge to bytes.
func (c *PoWChallenge) MarshalChallenge() []byte {
	buf := new(bytes.Buffer)
	buf.WriteByte(c.Difficulty)
	binary.Write(buf, binary.BigEndian, c.Timestamp.UnixNano())
	buf.Write([]byte(c.Service))
	buf.Write(c.Nonce[:])
	return buf.Bytes()
}

// GenerateChallenge creates a new PoWChallenge with a random nonce.
func GenerateChallenge(difficulty uint8, svc ServiceID) PoWChallenge {
	var nonce [8]byte
	rand.Read(nonce[:])
	return PoWChallenge{
		Difficulty: difficulty,
		Timestamp:  time.Now(),
		Service:    svc,
		Nonce:      nonce,
	}
}

// SolvePoW finds a nonce such that SHA3-256(challenge || nonce) has at least
// `difficulty` leading zero bytes.
func SolvePoW(challenge PoWChallenge) (PoWSolution, error) {
	start := time.Now()
	challengeBytes := challenge.MarshalChallenge()
	target := make([]byte, challenge.Difficulty)

	var nonce uint64
	for {
		var nonceBytes [8]byte
		binary.BigEndian.PutUint64(nonceBytes[:], nonce)

		h := crypto.SHA3Hash(append(challengeBytes, nonceBytes[:]...))
		if bytes.Equal(h[:challenge.Difficulty], target) {
			return PoWSolution{
				Nonce:    nonce,
				Hash:     h,
				Time:     time.Now(),
				Duration: time.Since(start),
			}, nil
		}

		nonce++
		if nonce == 0 {
			return PoWSolution{}, errors.New("nonce space exhausted")
		}
	}
}

// VerifyPoW checks that a solution satisfies the challenge.
func VerifyPoW(challenge PoWChallenge, sol PoWSolution) bool {
	challengeBytes := challenge.MarshalChallenge()
	var nonceBytes [8]byte
	binary.BigEndian.PutUint64(nonceBytes[:], sol.Nonce)

	h := crypto.SHA3Hash(append(challengeBytes, nonceBytes[:]...))
	if !bytes.Equal(h[:], sol.Hash[:]) {
		return false
	}
	target := make([]byte, challenge.Difficulty)
	return bytes.Equal(h[:challenge.Difficulty], target)
}

// PoWConfig tunes the proof-of-work subsystem.
type PoWConfig struct {
	BaseDifficulty      uint8
	MinDifficulty       uint8
	MaxDifficulty       uint8
	TargetSolveTime     time.Duration
	AdjustmentInterval  time.Duration
	MaxAdjustmentFactor float64
}

// DefaultPoWConfig returns sensible defaults.
func DefaultPoWConfig() PoWConfig {
	return PoWConfig{
		BaseDifficulty:      2,
		MinDifficulty:       1,
		MaxDifficulty:       5,
		TargetSolveTime:     100 * time.Millisecond,
		AdjustmentInterval:  5 * time.Minute,
		MaxAdjustmentFactor: 2.0,
	}
}

// DifficultyAdjuster manages dynamic PoW difficulty based on observed solve
// times.
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
