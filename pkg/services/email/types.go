package email

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/crypto"
	"github.com/ram1234598766-dotcom/Local-WEB/pkg/transport"
	"golang.org/x/crypto/pbkdf2"
)

// ServiceID is the 1-byte identifier for the email service.
const ServiceID = transport.ServiceID('E')

// Message represents an email message.
type Message struct {
	ID       string
	From     string
	To       []string
	CC       []string
	BCC      []string
	Subject  string
	Body     string
	Headers  map[string]string
	Received time.Time
	Size     int64
	UID      uint32
	Flags    MailFlags
	Maildir  string
	Filename string
}

// MailFlags represents IMAP message flags.
type MailFlags uint32

const (
	FlagSeen MailFlags = 1 << iota
	FlagAnswered
	FlagFlagged
	FlagDeleted
	FlagDraft
	FlagRecent
)

// SMTPState represents the SMTP session state.
type SMTPState int

const (
	SMTPStateGreeting SMTPState = iota
	SMTPStateEHLO
	SMTPStateMAIL
	SMTPStateRCPT
	SMTPStateDATA
	SMTPStateAUTH
	SMTPStateQuit
)

// IMAPState represents the IMAP session state.
type IMAPState int

const (
	IMAPStateNotAuth IMAPState = iota
	IMAPStateAuth
	IMAPStateSelected
)

// Mailbox represents a user's mailbox.
type Mailbox struct {
	Name        string
	Path        string
	UIDValidity uint32
	UIDNext     uint32
	Messages    []*Message
	mu          sync.RWMutex
}

// CredentialVerifier validates user credentials for SMTP/IMAP auth.
// Implementations MUST be constant-time to prevent timing attacks.
type CredentialVerifier interface {
	// Verify returns true if the (user, pass) pair is valid.
	Verify(user, pass string) bool
}

// CredentialStore is an in-memory credential store using PBKDF2-hashed passwords.
// It satisfies CredentialVerifier.
type CredentialStore struct {
	entries map[string]*credentialEntry
	mu      sync.RWMutex
}

type credentialEntry struct {
	salt      []byte
	hashedPwd []byte
}

const (
	pwdIterations = 100_000
	pwdKeyLen     = 32
)

// NewCredentialStore creates a new in-memory credential store.
func NewCredentialStore() *CredentialStore {
	return &CredentialStore{entries: make(map[string]*credentialEntry)}
}

// SetCredential stores a username/password pair, hashing the password with
// a random salt using PBKDF2-HMAC-SHA-256.
func (s *CredentialStore) SetCredential(user, pass string) error {
	if user == "" || pass == "" {
		return errors.New("username and password must be non-empty")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	hashed := pbkdf2Key(pass, salt)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[user] = &credentialEntry{salt: salt, hashedPwd: hashed}
	return nil
}

// Verify validates user credentials using constant-time comparison.
func (s *CredentialStore) Verify(user, pass string) bool {
	s.mu.RLock()
	entry, ok := s.entries[user]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	hashed := pbkdf2Key(pass, entry.salt)
	return subtle.ConstantTimeCompare(hashed, entry.hashedPwd) == 1
}

func pbkdf2Key(pass string, salt []byte) []byte {
	return pbkdf2.Key([]byte(pass), salt, pwdIterations, pwdKeyLen, sha256.New)
}

// QueueEntry represents an entry in the offline delivery queue.
type QueueEntry struct {
	ID          string
	Message     *Message
	PeerID      [32]byte
	Attempts    int
	MaxAttempts int
	NextRetry   time.Time
	Created     time.Time
	LastError   error
}

// Queue manages offline message delivery.
type Queue struct {
	entries []*QueueEntry
	mu      sync.RWMutex
}

// SMTPConfig holds SMTP server configuration.
type SMTPConfig struct {
	Hostname     string
	Port         int
	AuthRequired bool
	TLSEnabled   bool
	MaxSize      int64
	Listener     net.Listener
	Relay        *transport.Server
	DB           *MailboxStore
	Queue        *Queue
	PowChecker   *PoWChecker
	Credentials  CredentialVerifier
	TLSConfig    *tls.Config
}

// IMAPConfig holds IMAP server configuration.
type IMAPConfig struct {
	Hostname    string
	Port        int
	Listener    net.Listener
	DB          *MailboxStore
	Credentials CredentialVerifier
}

// MailboxStore manages maildir storage for all users.
type MailboxStore struct {
	basePath string
	users    map[string]*Mailbox
	mu       sync.RWMutex
}

// SMTPSession represents an SMTP session.
type SMTPSession struct {
	state      SMTPState
	config     *SMTPConfig
	from       string
	recipients []string
	authUser   string
	dataBuf    []byte
}

// IMAPSession represents an IMAP session.
type IMAPSession struct {
	state    IMAPState
	config   *IMAPConfig
	selected *Mailbox
	user     string
}

// PoWChecker manages proof-of-work verification.
type PoWChecker struct {
	difficulty int
	nonces     map[string]time.Time
	mu         sync.RWMutex
}

// GenerateID creates a unique message ID.
func GenerateID() string {
	buf := make([]byte, 16)
	rand.Read(buf)
	hash := crypto.SHA3Hash(buf)
	out := make([]byte, 12)
	copy(out, hash[:12])
	return hex.EncodeToString(out)
}

// GenerateQueueID creates a unique queue entry ID.
func GenerateQueueID() string {
	return GenerateID()
}

// Maildir constants
const (
	maildirNew = "new"
	maildirCur = "cur"
	maildirTmp = "tmp"
)

// NewMailboxStore creates a new mailbox store at the given path.
func NewMailboxStore(basePath string) *MailboxStore {
	return &MailboxStore{
		basePath: basePath,
		users:    make(map[string]*Mailbox),
	}
}

// GetOrCreateMailbox returns a user's mailbox, creating it if needed.
func (s *MailboxStore) GetOrCreateMailbox(user string) (*Mailbox, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if m, ok := s.users[user]; ok {
		return m, nil
	}

	userPath := filepath.Join(s.basePath, user)
	for _, sub := range []string{maildirNew, maildirCur, maildirTmp} {
		if err := os.MkdirAll(filepath.Join(userPath, sub), 0700); err != nil {
			return nil, fmt.Errorf("create maildir %s: %w", sub, err)
		}
	}

	m := &Mailbox{
		Name:        user,
		Path:        userPath,
		UIDValidity: uint32(time.Now().Unix()),
		UIDNext:     1,
	}
	s.users[user] = m
	return m, nil
}

// NewQueue creates a new offline delivery queue.
func NewQueue() *Queue {
	return &Queue{}
}

// Add adds a message to the queue.
func (q *Queue) Add(entry *QueueEntry) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.entries = append(q.entries, entry)
}

// Next returns the next entry ready for retry.
func (q *Queue) Next() *QueueEntry {
	q.mu.Lock()
	defer q.mu.Unlock()

	var next *QueueEntry
	for _, e := range q.entries {
		if e.Attempts >= e.MaxAttempts {
			continue
		}
		if next == nil || e.NextRetry.Before(next.NextRetry) {
			next = e
		}
	}
	return next
}

// Remove removes an entry from the queue.
func (q *Queue) Remove(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for idx, e := range q.entries {
		if e.ID == id {
			q.entries = append(q.entries[:idx], q.entries[idx+1:]...)
			return
		}
	}
}

// Count returns the number of queued entries.
func (q *Queue) Count() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.entries)
}

// FormatMaildirName generates a maildir-compatible filename.
func FormatMaildirName(unique string, host string) string {
	return fmt.Sprintf("%s.%s", time.Now().UTC().Format("20060102_150405.999999999"), host)
}

// ParseMaildirName parses a maildir filename to extract delivery time.
func ParseMaildirName(name string) (time.Time, error) {
	parts := strings.SplitN(name, ".", 2)
	if len(parts) < 2 {
		return time.Time{}, fmt.Errorf("invalid maildir name: %s", name)
	}
	timeStr := strings.TrimSuffix(parts[0], "_S")
	t, err := time.Parse("20060102_150405.999999999", timeStr)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}
