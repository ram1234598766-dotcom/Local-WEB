package email

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// StoreMessage stores a message in the user's maildir.
func (m *Mailbox) StoreMessage(msg *Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	filename := FormatMaildirName(msg.ID, m.Name)
	msg.Filename = filename
	msg.UID = m.UIDNext
	m.UIDNext++

	tmpPath := filepath.Join(m.Path, maildirTmp, filename)
	newPath := filepath.Join(m.Path, maildirNew, filename)

	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	_, err = f.WriteString(msg.String())
	f.Close()
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("write message: %w", err)
	}

	if err := os.Rename(tmpPath, newPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("move to new: %w", err)
	}

	msg.Maildir = newPath
	m.Messages = append(m.Messages, msg)
	return nil
}

// String formats the message for maildir storage.
func (m *Message) String() string {
	var sb strings.Builder
	written := make(map[string]bool)

	for k, v := range m.Headers {
		sb.WriteString(k)
		sb.WriteString(": ")
		sb.WriteString(v)
		sb.WriteString("\r\n")
		written[strings.ToLower(k)] = true
	}

	if !written["from"] && m.From != "" {
		sb.WriteString("From: ")
		sb.WriteString(m.From)
		sb.WriteString("\r\n")
	}
	if !written["to"] && len(m.To) > 0 {
		sb.WriteString("To: ")
		sb.WriteString(strings.Join(m.To, ", "))
		sb.WriteString("\r\n")
	}
	if !written["subject"] && m.Subject != "" {
		sb.WriteString("Subject: ")
		sb.WriteString(m.Subject)
		sb.WriteString("\r\n")
	}
	if !written["date"] && !m.Received.IsZero() {
		sb.WriteString("Date: ")
		sb.WriteString(m.Received.Format(time.RFC1123Z))
		sb.WriteString("\r\n")
	}

	sb.WriteString("\r\n")
	sb.WriteString(m.Body)
	return sb.String()
}

// LoadMessages loads all messages from the maildir.
func (m *Mailbox) LoadMessages() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Messages = m.Messages[:0]

	newDir := filepath.Join(m.Path, maildirNew)
	curDir := filepath.Join(m.Path, maildirCur)

	files, err := os.ReadDir(newDir)
	if err != nil {
		return fmt.Errorf("read new: %w", err)
	}

	for _, fi := range files {
		if fi.IsDir() {
			continue
		}
		path := filepath.Join(newDir, fi.Name())
		msg, err := readMessageFile(path)
		if err != nil {
			continue
		}
		msg.Filename = fi.Name()
		msg.Maildir = path
		msg.UID = m.UIDNext
		m.UIDNext++
		m.Messages = append(m.Messages, msg)
	}

	files, err = os.ReadDir(curDir)
	if err != nil {
		return fmt.Errorf("read cur: %w", err)
	}

	for _, fi := range files {
		if fi.IsDir() {
			continue
		}
		path := filepath.Join(curDir, fi.Name())
		msg, err := readMessageFile(path)
		if err != nil {
			continue
		}
		msg.Filename = fi.Name()
		msg.Maildir = path
		msg.UID = m.UIDNext
		m.UIDNext++
		m.Messages = append(m.Messages, msg)
	}

	return nil
}

func readMessageFile(path string) (*Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	msg := &Message{
		Headers: make(map[string]string),
	}
	msg.Received, _ = ParseMaildirName(filepath.Base(path))

	scanner := bufio.NewScanner(f)
	headerMode := true
	var body []string

	for scanner.Scan() {
		line := scanner.Text()
		if headerMode {
			if line == "" {
				headerMode = false
				continue
			}
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				msg.Headers[key] = val
				switch strings.ToLower(key) {
				case "from":
					msg.From = val
				case "to":
					msg.To = append(msg.To, parseAddresses(val)...)
				case "cc":
					msg.CC = append(msg.CC, parseAddresses(val)...)
				case "subject":
					msg.Subject = val
				}
			}
		} else {
			body = append(body, line)
		}
	}

	msg.Body = strings.Join(body, "\n")
	info, _ := f.Stat()
	msg.Size = info.Size()
	return msg, nil
}

func parseAddresses(s string) []string {
	var out []string
	for _, addr := range strings.Split(s, ",") {
		addr = strings.TrimSpace(addr)
		if addr != "" {
			out = append(out, addr)
		}
	}
	return out
}

// MoveToCur moves a message from new to cur.
func (m *Mailbox) MoveToCur(msg *Message, flags MailFlags) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if msg.Maildir == "" {
		return fmt.Errorf("message has no path")
	}

	curName := msg.Filename
	msg.Flags = flags

	curPath := filepath.Join(m.Path, maildirCur, curName)
	srcPath := msg.Maildir

	if err := os.Rename(srcPath, curPath); err != nil {
		return fmt.Errorf("move to cur: %w", err)
	}

	msg.Maildir = curPath
	msg.Filename = curName
	msg.Flags = flags
	return nil
}

// DeleteMessage removes a message from the mailbox.
func (m *Mailbox) DeleteMessage(msg *Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if msg.Maildir == "" {
		return nil
	}

	if err := os.Remove(msg.Maildir); err != nil {
		return err
	}

	for i, mm := range m.Messages {
		if mm == msg {
			m.Messages = append(m.Messages[:i], m.Messages[i+1:]...)
			break
		}
	}
	msg.Maildir = ""
	return nil
}

// GetByUID returns a message by UID.
func (m *Mailbox) GetByUID(uid uint32) *Message {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, msg := range m.Messages {
		if msg.UID == uid {
			return msg
		}
	}
	return nil
}

// MessageCount returns the number of messages.
func (m *Mailbox) MessageCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.Messages)
}

// RecentCount returns the number of recent messages.
func (m *Mailbox) RecentCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, msg := range m.Messages {
		if msg.Flags&FlagRecent != 0 {
			count++
		}
	}
	return count
}

// UnseenCount returns the number of unseen messages.
func (m *Mailbox) UnseenCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, msg := range m.Messages {
		if msg.Flags&FlagSeen == 0 {
			count++
		}
	}
	return count
}

// Expunge removes all messages marked as deleted.
func (m *Mailbox) Expunge() []*Message {
	m.mu.Lock()
	defer m.mu.Unlock()

	var deleted []*Message
	var remaining []*Message

	for _, msg := range m.Messages {
		if msg.Flags&FlagDeleted != 0 {
			deleted = append(deleted, msg)
			os.Remove(msg.Maildir)
		} else {
			remaining = append(remaining, msg)
		}
	}

	m.Messages = remaining
	return deleted
}

// SyncToDisk syncs the mailbox state.
func (m *Mailbox) SyncToDisk() error {
	return nil
}

// CopyTo copies a message to another mailbox.
func (m *Mailbox) CopyTo(msg *Message, dest *Mailbox, flags MailFlags) error {
	clone := *msg
	clone.Flags = flags
	clone.Maildir = ""
	clone.Filename = ""
	return dest.StoreMessage(&clone)
}

// Append appends a raw message to the mailbox.
func (m *Mailbox) Append(raw []byte) (*Message, error) {
	msg, err := ParseMessage(raw)
	if err != nil {
		return nil, err
	}
	msg.ID = GenerateID()
	return msg, m.StoreMessage(msg)
}

// ParseMessage parses a raw RFC 5322 message.
func ParseMessage(data []byte) (*Message, error) {
	msg := &Message{
		Headers: make(map[string]string),
	}

	scanner := bufio.NewScanner(&bytesReader{data})
	headerMode := true
	var body []byte

	for scanner.Scan() {
		line := scanner.Text()
		if headerMode {
			if line == "" {
				headerMode = false
				continue
			}
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				msg.Headers[key] = val
				switch strings.ToLower(key) {
				case "from":
					msg.From = val
				case "to":
					msg.To = append(msg.To, parseAddresses(val)...)
				case "cc":
					msg.CC = append(msg.CC, parseAddresses(val)...)
				case "subject":
					msg.Subject = val
				case "date":
					if t, err := time.Parse(time.RFC1123Z, val); err == nil {
						msg.Received = t
					}
				}
			}
		} else {
			body = append(body, scanner.Bytes()...)
			body = append(body, '\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan message: %w", err)
	}

	if msg.Received.IsZero() {
		msg.Received = time.Now()
	}
	msg.Body = string(body)
	msg.Size = int64(len(data))
	return msg, nil
}

type bytesReader struct {
	data []byte
}

func (b *bytesReader) Read(p []byte) (int, error) {
	if len(b.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b.data)
	b.data = b.data[n:]
	return n, nil
}

func init() {
	_ = bufio.ScanBytes
}
