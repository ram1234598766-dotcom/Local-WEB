package security

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/sha3"
)

// Proof-of-work parameters and hard limits.
//
// A solution is valid when SHA3-256(workSeed || nonce) has at least
// `Difficulty` leading zero BITS. Work is exponential in Difficulty, so the
// difficulty unit is bits and the security cost is 2^difficulty hashes.
//
// The memory-hardness comes from a single Argon2id call per challenge that
// derives `workSeed`. That call is paid once by the solver and once by the
// verifier; the nonce search over SHA3-256 is then cheap and parallelisable.
// Running Argon2id per nonce instead would make the search infeasible long
// before it became expensive to attack.
//
// The caps below are a denial-of-service control, not tuning: a challenge is
// attacker-controlled data, so the requested cost is clamped before any
// allocation happens.
const (
	// DefaultPoWMemoryKiB is the Argon2id memory cost advertised in challenges.
	DefaultPoWMemoryKiB = 64 * 1024
	// DefaultPoWTimeCost is the Argon2id iteration count. One pass is enough
	// because the derived seed is what the (unbounded) nonce search runs on.
	DefaultPoWTimeCost = 1
	// DefaultPoWParallelism is the Argon2id lane count. One lane maximises
	// memory-hardness per allocated MiB.
	DefaultPoWParallelism uint8 = 1

	// MaxPoWMemoryKiB caps the memory a remote peer can make us allocate.
	MaxPoWMemoryKiB = 64 * 1024
	// MaxPoWTimeCost caps Argon2id iterations per verification.
	MaxPoWTimeCost uint32 = 4
	// MaxPoWParallelism caps Argon2id lanes per verification.
	MaxPoWParallelism uint8 = 4

	// MinDifficulty and MaxDifficulty bound the advertised work so a
	// malicious issuer cannot demand work that is infeasible to perform.
	MinDifficulty uint8 = 8
	MaxDifficulty uint8 = 24

	// PowReplayWindow is how long a solved challenge stays valid.
	PowReplayWindow = 5 * time.Minute

	// powNonceSearchLimit bounds the nonce search so a difficulty that is
	// unreachable in practice fails fast instead of spinning forever.
	powNonceSearchLimit uint64 = 1 << 32
)

// PoWChallenge is a proof-of-work challenge. All cost parameters are hints
// that the verifier clamps to the caps above before allocating.
type PoWChallenge struct {
	Algorithm   string // "argon2id-sha3"
	Difficulty  uint8  // required leading zero bits of SHA3-256(workSeed||nonce)
	Memory      uint32 // Argon2id memory cost in KiB (clamped to MaxPoWMemoryKiB)
	TimeCost    uint32 // Argon2id iteration count (clamped to MaxPoWTimeCost)
	Parallelism uint8  // Argon2id lanes (clamped to MaxPoWParallelism)
	Timestamp   time.Time
	Service     ServiceID
	Salt        [16]byte
}

// PoWSolution is a valid response to a PoWChallenge.
type PoWSolution struct {
	Nonce    [8]byte
	Hash     [32]byte // SHA3-256(workSeed || nonce)
	Time     time.Time
	Duration time.Duration
}

// MarshalChallenge serialises a PoWChallenge to bytes. The encoding is stable:
// it is hashed by both solver and verifier, so any field order change is a
// protocol break.
func (c *PoWChallenge) MarshalChallenge() []byte {
	buf := new(bytes.Buffer)
	buf.WriteString(c.Algorithm)
	buf.WriteByte(c.Difficulty)
	binary.Write(buf, binary.BigEndian, c.Memory)
	binary.Write(buf, binary.BigEndian, c.TimeCost)
	buf.WriteByte(c.Parallelism)
	binary.Write(buf, binary.BigEndian, c.Timestamp.UnixNano())
	buf.Write([]byte(c.Service))
	buf.Write(c.Salt[:])
	return buf.Bytes()
}

// clamp returns the cost parameters actually used, bounded by the hard caps.
// It is applied on both the solving and verifying paths so a peer cannot make
// us allocate memory or CPU that the challenge did not legitimately need.
func (c *PoWChallenge) clamp() (memory uint32, timeCost uint32, parallelism uint8) {
	memory = c.Memory
	if memory == 0 || memory > MaxPoWMemoryKiB {
		memory = MaxPoWMemoryKiB
	}
	// Argon2 requires at least 8*p blocks of memory.
	timeCost = c.TimeCost
	if timeCost == 0 || timeCost > MaxPoWTimeCost {
		timeCost = MaxPoWTimeCost
	}
	parallelism = c.Parallelism
	if parallelism == 0 || parallelism > MaxPoWParallelism {
		parallelism = MaxPoWParallelism
	}
	if min := uint32(8) * uint32(parallelism); memory < min {
		memory = min
	}
	return memory, timeCost, parallelism
}

// workSeed derives the memory-hard seed the nonce search runs on. The Argon2id
// parameters are taken from the challenge after clamping.
func (c *PoWChallenge) workSeed() [32]byte {
	memory, timeCost, parallelism := c.clamp()
	h := argon2.IDKey(c.MarshalChallenge(), c.Salt[:], timeCost, memory, parallelism, 32)
	var seed [32]byte
	copy(seed[:], h)
	return seed
}

// candidateHash computes SHA3-256(workSeed || nonce) with an 8-byte
// big-endian nonce, the single definition of "a hash" used by both paths.
func candidateHash(seed [32]byte, nonce [8]byte) [32]byte {
	var nonceBytes [8]byte
	binary.BigEndian.PutUint64(nonceBytes[:], binary.BigEndian.Uint64(nonce[:]))
	h := sha3.New256()
	h.Write(seed[:])
	h.Write(nonceBytes[:])
	var out [32]byte
	h.Sum(out[:0])
	return out
}

// leadingZeroBits counts the leading zero bits of a hash, capped at 256.
func leadingZeroBits(hash [32]byte) int {
	n := 0
	for _, b := range hash {
		if b == 0 {
			n += 8
			continue
		}
		for i := 7; i >= 0; i-- {
			if b&(1<<uint(i)) == 0 {
				n++
			} else {
				return n
			}
		}
		return n
	}
	return n
}

// GenerateChallenge creates a new PoW challenge with a random salt.
// Difficulty is clamped to the supported range; out-of-range values are a
// caller error and are corrected rather than accepted.
func GenerateChallenge(difficulty uint8, svc ServiceID) PoWChallenge {
	var salt [16]byte
	rand.Read(salt[:])
	if difficulty < MinDifficulty {
		difficulty = MinDifficulty
	}
	if difficulty > MaxDifficulty {
		difficulty = MaxDifficulty
	}
	return PoWChallenge{
		Algorithm:   "argon2id-sha3",
		Difficulty:  difficulty,
		Memory:      DefaultPoWMemoryKiB,
		TimeCost:    DefaultPoWTimeCost,
		Parallelism: DefaultPoWParallelism,
		Timestamp:   time.Now(),
		Service:     svc,
		Salt:        salt,
	}
}

// SolvePoW finds a nonce such that SHA3-256(workSeed || nonce) has at least
// the challenge's Difficulty leading zero bits.
//
// The one expensive Argon2id call happens before the loop. The loop itself is
// pure SHA3-256, which is what makes 2^difficulty work tractable.
func SolvePoW(challenge PoWChallenge) (PoWSolution, error) {
	return SolvePoWContext(context.Background(), challenge)
}

// SolvePoWContext is SolvePoW with cancellation, so a caller shutting down
// does not block on an in-flight nonce search.
func SolvePoWContext(ctx context.Context, challenge PoWChallenge) (PoWSolution, error) {
	if err := ctx.Err(); err != nil {
		return PoWSolution{}, err
	}
	if challenge.Difficulty > MaxDifficulty {
		return PoWSolution{}, errors.New("pow difficulty above maximum")
	}

	start := time.Now()
	seed := challenge.workSeed()
	var nonce uint64
	for nonce < powNonceSearchLimit {
		if nonce&0xffff == 0 {
			if err := ctx.Err(); err != nil {
				return PoWSolution{}, err
			}
		}
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], nonce)
		hash := candidateHash(seed, n)
		if leadingZeroBits(hash) >= int(challenge.Difficulty) {
			return PoWSolution{
				Nonce:    n,
				Hash:     hash,
				Time:     time.Now(),
				Duration: time.Since(start),
			}, nil
		}
		nonce++
	}
	return PoWSolution{}, errors.New("pow nonce space exhausted")
}

// VerifyPoW checks that a solution satisfies the challenge. It recomputes the
// work seed and the candidate hash, so a forged Hash field is rejected even
// when the nonce would otherwise work.
func VerifyPoW(challenge PoWChallenge, sol PoWSolution) bool {
	seed := challenge.workSeed()
	computed := candidateHash(seed, sol.Nonce)
	if subtle.ConstantTimeCompare(computed[:], sol.Hash[:]) != 1 {
		return false
	}
	return leadingZeroBits(computed) >= int(challenge.Difficulty)
}

// VerifyPoWWithTime is VerifyPoW plus a freshness check on the challenge
// timestamp. Challenge freshness is enforced by the validator, not by
// VerifyPoW, so offline verification of an archived solution stays possible.
func VerifyPoWWithTime(challenge PoWChallenge, sol PoWSolution, now time.Time) bool {
	if !VerifyPoW(challenge, sol) {
		return false
	}
	age := now.Sub(challenge.Timestamp)
	return age <= PowReplayWindow && age >= -PowReplayWindow
}

// PoWConfig tunes the proof-of-work subsystem.
type PoWConfig struct {
	BaseDifficulty      uint8
	MinDifficulty       uint8
	MaxDifficulty       uint8
	TargetSolveTime     time.Duration
	AdjustmentInterval  time.Duration
	MaxAdjustmentFactor float64
	Memory              uint32 // Argon2id memory in KiB
	TimeCost            uint32 // Argon2id iterations
	Parallelism         uint8
}

// DefaultPoWConfig returns sensible defaults for the Argon2id+SHA3 PoW.
// BaseDifficulty 16 bits is ~65k SHA3-256 hashes, roughly 30ms on a modern
// core, plus the ~40ms Argon2id seed derivation.
func DefaultPoWConfig() PoWConfig {
	return PoWConfig{
		BaseDifficulty:      16,
		MinDifficulty:       MinDifficulty,
		MaxDifficulty:       MaxDifficulty,
		TargetSolveTime:     100 * time.Millisecond,
		AdjustmentInterval:  5 * time.Minute,
		MaxAdjustmentFactor: 2.0,
		Memory:              DefaultPoWMemoryKiB,
		TimeCost:            DefaultPoWTimeCost,
		Parallelism:         DefaultPoWParallelism,
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
//
// Solve time is exponential in difficulty (2^d hashes), so the correction is
// logarithmic in the time ratio: shifting difficulty by log2(target/avg)
// multiplies the expected solve time by avg/target.
func (a *DifficultyAdjuster) RecordSolve(d time.Duration) uint8 {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.history = append(a.history, d)
	if len(a.history) > 100 {
		a.history = a.history[len(a.history)-100:]
	}

	if a.config.TargetSolveTime <= 0 {
		return a.difficulty
	}
	// Adjust on the first solve, then only once per adjustment interval.
	if !a.lastAdjust.IsZero() && time.Since(a.lastAdjust) < a.config.AdjustmentInterval {
		return a.difficulty
	}

	avg := averageDuration(a.history)
	if avg <= 0 {
		return a.difficulty
	}

	ratio := float64(a.config.TargetSolveTime) / float64(avg)
	if a.config.MaxAdjustmentFactor > 0 {
		// Bound the correction so one slow sample cannot collapse the
		// difficulty and make the subsystem trivially cheap to attack.
		lo := 1 / a.config.MaxAdjustmentFactor
		hi := a.config.MaxAdjustmentFactor
		if ratio < lo {
			ratio = lo
		}
		if ratio > hi {
			ratio = hi
		}
	}

	newDiff := float64(a.difficulty) + math.Log2(ratio)
	if newDiff < float64(a.config.MinDifficulty) {
		newDiff = float64(a.config.MinDifficulty)
	}
	if newDiff > float64(a.config.MaxDifficulty) {
		newDiff = float64(a.config.MaxDifficulty)
	}

	a.difficulty = uint8(math.Round(newDiff))
	a.lastAdjust = time.Now()

	log.Info().
		Uint8("difficulty", a.difficulty).
		Dur("avg", avg).
		Dur("target", a.config.TargetSolveTime).
		Msg("PoW difficulty adjusted")

	return a.difficulty
}

// CurrentDifficulty returns the current difficulty level in bits.
func (a *DifficultyAdjuster) CurrentDifficulty() uint8 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.difficulty
}

// PoWValidator validates PoW challenges and solutions for incoming messages.
type PoWValidator struct {
	mu       sync.RWMutex
	adjuster *DifficultyAdjuster
	seen     map[string]time.Time // challenge digest -> when it was accepted
}

// NewPoWValidator creates a validator backed by a difficulty adjuster.
func NewPoWValidator(adjuster *DifficultyAdjuster) *PoWValidator {
	return &PoWValidator{
		adjuster: adjuster,
		seen:     make(map[string]time.Time),
	}
}

// Validate checks a PoW solution and records the attempt.
//
// Replay protection: a challenge digest is accepted at most once per replay
// window, so a solution captured off the wire cannot be resubmitted to keep
// one expensive solve alive for the whole window.
func (v *PoWValidator) Validate(challenge PoWChallenge, sol PoWSolution) error {
	now := time.Now()
	if !VerifyPoWWithTime(challenge, sol, now) {
		return errors.New("invalid proof-of-work")
	}

	digest := crypto.SHA3Hash(challenge.MarshalChallenge())
	key := string(digest[:])

	v.mu.Lock()
	if last, ok := v.seen[key]; ok && now.Sub(last) < PowReplayWindow {
		v.mu.Unlock()
		return errors.New("proof-of-work replayed")
	}
	v.seen[key] = now
	// Bound the replay cache so a flood of distinct challenges cannot grow it
	// without limit.
	if len(v.seen) > 1024 {
		cutoff := now.Add(-PowReplayWindow)
		for k, t := range v.seen {
			if t.Before(cutoff) {
				delete(v.seen, k)
			}
		}
		// Still full of fresh entries: drop the oldest until under the cap.
		for len(v.seen) > 1024 {
			var oldestKey string
			var oldest time.Time
			for k, t := range v.seen {
				if oldestKey == "" || t.Before(oldest) {
					oldestKey, oldest = k, t
				}
			}
			if oldestKey == "" {
				break
			}
			delete(v.seen, oldestKey)
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
