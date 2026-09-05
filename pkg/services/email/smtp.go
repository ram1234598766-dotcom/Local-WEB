package email

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/ram1234598766-dotcom/Local-WEB/pkg/transport"
	"github.com/rs/zerolog/log"
)

// SMTPServer represents an SMTP server.
type SMTPServer struct {
	config  *SMTPConfig
	ln      net.Listener
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
	started time.Time
}

// NewSMTPServer creates a new SMTP server.
func NewSMTPServer(ctx context.Context, config *SMTPConfig) (*SMTPServer, error) {
	if config.Hostname == "" {
		config.Hostname = "localweb"
	}

	var ln net.Listener
	var err error
	if config.Listener != nil {
		ln = config.Listener
	} else {
		ln, err = net.Listen("tcp", fmt.Sprintf("%s:%d", config.Hostname, config.Port))
		if err != nil {
			return nil, fmt.Errorf("listen: %w", err)
		}
	}

	sctx, cancel := context.WithCancel(ctx)
	s := &SMTPServer{
		config:  config,
		ln:      ln,
		ctx:     sctx,
		cancel:  cancel,
		started: time.Now(),
	}

	go s.acceptLoop()
	log.Info().Str("addr", ln.Addr().String()).Msg("SMTP server started")
	return s, nil
}

func (s *SMTPServer) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				log.Warn().Err(err).Msg("SMTP accept error")
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(conn)
		}()
	}
}

func (s *SMTPServer) handleConn(conn net.Conn) {
	defer conn.Close()

	session := &SMTPSession{
		state:  SMTPStateGreeting,
		config: s.config,
	}

	reader := bufio.NewReader(conn)
	writeLine(conn, "220 "+s.config.Hostname+" ESMTP LocalWEB")

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}

		// STARTTLS replaces the connection with a TLS-wrapped one.
		if strings.ToUpper(strings.SplitN(line, " ", 2)[0]) == "STARTTLS" {
			if s.config.TLSEnabled && s.config.TLSConfig != nil {
				writeLine(conn, "220 Ready to start TLS")
				tlsConn := tls.Server(conn, s.config.TLSConfig)
				if err := tlsConn.Handshake(); err != nil {
					log.Warn().Err(err).Msg("SMTP TLS handshake failed")
					return
				}
				conn = tlsConn
				reader = bufio.NewReader(conn)
				session.state = SMTPStateEHLO
				continue
			}
			writeLine(conn, "454 TLS not available")
			continue
		}

		if err := s.handleCommand(session, conn, reader, line); err != nil {
			log.Warn().Err(err).Str("cmd", line).Msg("SMTP command error")
			return
		}

		if session.state == SMTPStateQuit {
			return
		}
	}
}

func (s *SMTPServer) handleCommand(session *SMTPSession, conn net.Conn, reader *bufio.Reader, line string) error {
	parts := strings.SplitN(line, " ", 2)
	cmd := strings.ToUpper(parts[0])
	arg := ""
	if len(parts) > 1 {
		arg = parts[1]
	}

	switch cmd {
	case "EHLO", "HELO":
		return s.cmdEHLO(session, conn, arg)
	case "AUTH":
		return s.cmdAUTH(session, conn, reader, arg)
	case "MAIL":
		return s.cmdMAIL(session, conn, arg)
	case "RCPT":
		return s.cmdRCPT(session, conn, arg)
	case "DATA":
		return s.cmdDATA(session, conn, reader)
	case "RSET":
		return s.cmdRSET(session, conn)
	case "NOOP":
		writeLine(conn, "250 OK")
		return nil
	case "QUIT":
		writeLine(conn, "221 Bye")
		session.state = SMTPStateQuit
		return nil
	default:
		writeLine(conn, "500 Unknown command")
		return nil
	}
}

func (s *SMTPServer) cmdEHLO(session *SMTPSession, conn net.Conn, arg string) error {
	if arg == "" {
		writeLine(conn, "501 Syntax: EHLO hostname")
		return nil
	}
	session.state = SMTPStateEHLO
	authMechanisms := "PLAIN"
	if s.config.Credentials != nil {
		authMechanisms = "PLAIN LOGIN CRAM-MD5"
	}
	writeLine(conn, "250-"+s.config.Hostname+" greets "+arg)
	if s.config.TLSEnabled {
		writeLine(conn, "250-STARTTLS")
	}
	writeLine(conn, "250-AUTH "+authMechanisms)
	writeLine(conn, "250-8BITMIME")
	writeLine(conn, "250 OK")
	return nil
}

func (s *SMTPServer) cmdAUTH(session *SMTPSession, conn net.Conn, reader *bufio.Reader, arg string) error {
	parts := strings.SplitN(arg, " ", 2)
	mechanism := strings.ToUpper(parts[0])
	param := ""
	if len(parts) > 1 {
		param = parts[1]
	}

	switch mechanism {
	case "PLAIN":
		data, err := base64.StdEncoding.DecodeString(param)
		if err != nil {
			writeLine(conn, "501 Invalid base64")
			return nil
		}
		fields := strings.SplitN(string(data), "\x00", 3)
		if len(fields) != 3 {
			writeLine(conn, "501 Invalid auth data")
			return nil
		}
		user := fields[1]
		pass := fields[2]
		if s.config.AuthRequired && !s.verifyCredentials(user, pass) {
			writeLine(conn, "535 Authentication failed")
			return nil
		}
		session.authUser = user
		session.state = SMTPStateAUTH
		writeLine(conn, "235 Authenticated")

	case "LOGIN":
		if param == "" {
			writeLine(conn, "334 VXNlcm5hbWU6")
			user, _ := reader.ReadString('\n')
			user = strings.TrimRight(user, "\r\n")
			writeLine(conn, "334 UGFzc3dvcmQ6")
			pass, _ := reader.ReadString('\n')
			pass = strings.TrimRight(pass, "\r\n")
			if s.config.AuthRequired && !s.verifyCredentials(user, pass) {
				writeLine(conn, "535 Authentication failed")
				return nil
			}
			session.authUser = user
			session.state = SMTPStateAUTH
			writeLine(conn, "235 Authenticated")
		} else {
			data, _ := base64.StdEncoding.DecodeString(param)
			user := string(data)
			writeLine(conn, "334 UGFzc3dvcmQ6")
			pass, _ := reader.ReadString('\n')
			pass = strings.TrimRight(pass, "\r\n")
			if s.config.AuthRequired && !s.verifyCredentials(user, pass) {
				writeLine(conn, "535 Authentication failed")
				return nil
			}
			session.authUser = user
			session.state = SMTPStateAUTH
			writeLine(conn, "235 Authenticated")
		}

	case "CRAM-MD5":
		challenge := "<" + generateRandomString(16) + "@" + s.config.Hostname + ">"
		encoded := base64.StdEncoding.EncodeToString([]byte(challenge))
		writeLine(conn, "334 "+encoded)
		response, _ := reader.ReadString('\n')
		response = strings.TrimRight(response, "\r\n")
		data, _ := base64.StdEncoding.DecodeString(response)
		parts := strings.SplitN(string(data), " ", 2)
		if len(parts) != 2 {
			writeLine(conn, "501 Invalid auth data")
			return nil
		}
		user := parts[0]
		proof := parts[1]
		if s.config.AuthRequired && !s.verifyCramMD5(user, challenge, proof) {
			writeLine(conn, "535 Authentication failed")
			return nil
		}
		session.authUser = user
		session.state = SMTPStateAUTH
		writeLine(conn, "235 Authenticated")

	default:
		writeLine(conn, "504 Unsupported AUTH mechanism")
		return nil
	}

	return nil
}

func (s *SMTPServer) cmdMAIL(session *SMTPSession, conn net.Conn, arg string) error {
	if session.state != SMTPStateEHLO && session.state != SMTPStateAUTH && session.state != SMTPStateMAIL && session.state != SMTPStateRCPT {
		writeLine(conn, "503 Bad sequence of commands")
		return nil
	}

	if !strings.HasPrefix(strings.ToUpper(arg), "FROM:") {
		writeLine(conn, "501 Syntax: MAIL FROM:<address>")
		return nil
	}

	addr := strings.TrimSpace(arg[5:])
	addr = strings.Trim(addr, "<>")
	session.from = addr
	session.state = SMTPStateMAIL
	writeLine(conn, "250 OK")
	return nil
}

func (s *SMTPServer) cmdRCPT(session *SMTPSession, conn net.Conn, arg string) error {
	if session.state != SMTPStateMAIL && session.state != SMTPStateRCPT {
		writeLine(conn, "503 Bad sequence of commands")
		return nil
	}

	if !strings.HasPrefix(strings.ToUpper(arg), "TO:") {
		writeLine(conn, "501 Syntax: RCPT TO:<address>")
		return nil
	}

	addr := strings.TrimSpace(arg[3:])
	addr = strings.Trim(addr, "<>")
	session.recipients = append(session.recipients, addr)
	session.state = SMTPStateRCPT
	writeLine(conn, "250 OK")
	return nil
}

func (s *SMTPServer) cmdDATA(session *SMTPSession, conn net.Conn, reader *bufio.Reader) error {
	if session.state != SMTPStateRCPT {
		writeLine(conn, "503 Bad sequence of commands")
		return nil
	}

	writeLine(conn, "354 Start mail input; end with <CRLF>.<CRLF>")

	var buf bytes.Buffer
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "." {
			break
		}
		if strings.HasPrefix(line, "..") {
			line = line[1:]
		}
		buf.WriteString(line)
		buf.WriteString("\r\n")
	}

	msg, err := ParseMessage(buf.Bytes())
	if err != nil {
		writeLine(conn, "550 Failed to parse message")
		return nil
	}

	msg.From = session.from
	msg.To = session.recipients
	msg.ID = GenerateID()
	msg.Received = time.Now()
	msg.Headers["Received"] = fmt.Sprintf("from %s by %s with ESMTP", session.from, s.config.Hostname)

	if s.config.PowChecker != nil {
		if powHeader, ok := msg.Headers["X-PoW"]; ok {
			parts := strings.SplitN(powHeader, ":", 2)
			if len(parts) == 2 {
				if !s.config.PowChecker.VerifyPoW(msg.From, strings.TrimSpace(parts[1])) {
					writeLine(conn, "550 PoW verification failed")
					return nil
				}
			}
		}
	}

	if IsSpam(msg) {
		writeLine(conn, "550 Message rejected as spam")
		return nil
	}

	for _, rcpt := range session.recipients {
		parts := strings.SplitN(rcpt, "@", 2)
		if len(parts) == 2 {
			user := parts[0]
			mb, err := s.config.DB.GetOrCreateMailbox(user)
			if err != nil {
				log.Warn().Err(err).Str("user", user).Msg("failed to get mailbox")
				continue
			}
			log.Info().Str("user", user).Str("mb_ptr", fmt.Sprintf("%p", mb)).Int("msg_count", mb.MessageCount()).Str("path", mb.Path).Msg("mailbox info")
			msgCopy := *msg
			if err := mb.StoreMessage(&msgCopy); err != nil {
				log.Warn().Err(err).Msg("failed to store message")
			} else {
				log.Info().Str("user", user).Str("msgid", msgCopy.ID).Str("maildir", msgCopy.Maildir).Msg("message stored")
			}
		}
	}

	session.state = SMTPStateMAIL
	writeLine(conn, "250 Message accepted")
	return nil
}

func (s *SMTPServer) cmdRSET(session *SMTPSession, conn net.Conn) error {
	session.from = ""
	session.recipients = nil
	session.dataBuf = nil
	session.state = SMTPStateEHLO
	writeLine(conn, "250 OK")
	return nil
}

func (s *SMTPServer) verifyCredentials(user, pass string) bool {
	if s.config.Credentials == nil {
		return false
	}
	return s.config.Credentials.Verify(user, pass)
}

func (s *SMTPServer) verifyCramMD5(user, challenge, proof string) bool {
	if s.config.Credentials == nil {
		return false
	}
	store, ok := s.config.Credentials.(*CredentialStore)
	if !ok {
		return false
	}
	store.mu.RLock()
	entry, exists := store.entries[user]
	store.mu.RUnlock()
	if !exists {
		return false
	}
	digest := hmac.New(md5.New, entry.hashedPwd)
	digest.Write([]byte(challenge))
	expected := hex.EncodeToString(digest.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(proof)) == 1
}

func writeLine(w interface {
	Write([]byte) (int, error)
}, line string) {
	w.Write([]byte(line + "\r\n"))
}

func readLine(r interface {
	Read([]byte) (int, error)
}) (string, error) {
	var buf bytes.Buffer
	tmp := make([]byte, 1)
	for {
		n, err := r.Read(tmp)
		if n == 1 {
			if tmp[0] == '\n' {
				break
			}
			buf.WriteByte(tmp[0])
		}
		if err != nil {
			return buf.String(), err
		}
	}
	return buf.String(), nil
}

func generateRandomString(n int) string {
	buf := make([]byte, n)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}

// DeliverRemote delivers a message to a remote peer via QUIC.
func DeliverRemote(msg *Message, peerID [32]byte, server *transport.Server) error {
	if server == nil {
		return errors.New("no transport server")
	}

	if err := server.SendTo(context.Background(), peerID, ServiceID, transport.MsgStore, []byte(msg.String())); err != nil {
		return fmt.Errorf("deliver via QUIC: %w", err)
	}
	return nil
}
