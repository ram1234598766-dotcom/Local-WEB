package email

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// IMAPServer represents an IMAP server.
type IMAPServer struct {
	config *IMAPConfig
	ln     net.Listener
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

// NewIMAPServer creates a new IMAP server.
func NewIMAPServer(ctx context.Context, config *IMAPConfig) (*IMAPServer, error) {
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
	s := &IMAPServer{
		config: config,
		ln:     ln,
		ctx:    sctx,
		cancel: cancel,
	}

	go s.acceptLoop()
	log.Info().Str("addr", ln.Addr().String()).Msg("IMAP server started")
	return s, nil
}

func (s *IMAPServer) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				log.Warn().Err(err).Msg("IMAP accept error")
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

func (s *IMAPServer) handleConn(conn net.Conn) {
	defer conn.Close()

	session := &IMAPSession{
		state: IMAPStateNotAuth,
	}

	reader := bufio.NewReader(conn)
	writeLine(conn, "* OK LocalWEB IMAP4rev2 server ready")

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}

		tag, cmd, arg, err := parseIMAPCommand(line)
		if err != nil {
			writeLine(conn, fmt.Sprintf("%s BAD %s", tag, err))
			continue
		}

		if err := s.handleCommand(session, conn, tag, cmd, arg); err != nil {
			log.Warn().Err(err).Str("cmd", cmd).Msg("IMAP command error")
		}
	}
}

func parseIMAPCommand(line string) (tag, cmd, arg string, err error) {
	parts := strings.SplitN(line, " ", 2)
	if len(parts) < 1 {
		return "", "", "", errors.New("empty command")
	}
	tag = parts[0]
	if len(parts) == 2 {
		rest := parts[1]
		idx := strings.Index(rest, " ")
		if idx >= 0 {
			cmd = strings.ToUpper(rest[:idx])
			arg = rest[idx+1:]
		} else {
			cmd = strings.ToUpper(rest)
		}
	}
	return tag, cmd, arg, nil
}

func (s *IMAPServer) handleCommand(session *IMAPSession, conn net.Conn, tag, cmd, arg string) error {
	switch cmd {
	case "CAPABILITY":
		return s.cmdCapability(session, conn, tag)
	case "LOGIN":
		return s.cmdLogin(session, conn, tag, arg)
	case "LOGOUT":
		return s.cmdLogout(session, conn, tag)
	case "SELECT":
		return s.cmdSelect(session, conn, tag, arg)
	case "EXAMINE":
		return s.cmdExamine(session, conn, tag, arg)
	case "FETCH":
		return s.cmdFetch(session, conn, tag, arg, false)
	case "STORE":
		return s.cmdStore(session, conn, tag, arg)
	case "SEARCH":
		return s.cmdSearch(session, conn, tag, arg)
	case "UID":
		return s.cmdUID(session, conn, tag, arg)
	case "IDLE":
		return s.cmdIdle(session, conn, tag)
	case "LIST":
		return s.cmdList(session, conn, tag, arg)
	case "STATUS":
		return s.cmdStatus(session, conn, tag, arg)
	case "CREATE", "DELETE", "RENAME", "SUBSCRIBE", "UNSUBSCRIBE", "LSUB", "X-IMAP4":
		writeLine(conn, fmt.Sprintf("%s OK %s completed", tag, cmd))
		return nil
	default:
		writeLine(conn, fmt.Sprintf("%s BAD Unknown command %s", tag, cmd))
		return nil
	}
}

func (s *IMAPServer) cmdCapability(session *IMAPSession, conn net.Conn, tag string) error {
	writeLine(conn, "* CAPABILITY IMAP4rev2 AUTH=PLAIN AUTH=LOGIN STARTTLS IDLE")
	writeLine(conn, fmt.Sprintf("%s OK CAPABILITY completed", tag))
	return nil
}

func (s *IMAPServer) cmdLogin(session *IMAPSession, conn net.Conn, tag, arg string) error {
	parts := strings.SplitN(arg, " ", 2)
	if len(parts) != 2 {
		writeLine(conn, fmt.Sprintf("%s BAD Syntax: LOGIN user password", tag))
		return nil
	}

	user := parts[0]
	password := parts[1]

	if password == "" {
		writeLine(conn, fmt.Sprintf("%s NO Login failure", tag))
		return nil
	}

	session.user = user
	session.state = IMAPStateAuth
	writeLine(conn, fmt.Sprintf("%s OK LOGIN completed", tag))
	return nil
}

func (s *IMAPServer) cmdLogout(session *IMAPSession, conn net.Conn, tag string) error {
	writeLine(conn, "* BYE LocalWEB IMAP4rev2 server logging out")
	writeLine(conn, fmt.Sprintf("%s OK LOGOUT completed", tag))
	return nil
}

func (s *IMAPServer) cmdSelect(session *IMAPSession, conn net.Conn, tag, arg string) error {
	if session.state != IMAPStateAuth && session.state != IMAPStateSelected {
		writeLine(conn, fmt.Sprintf("%s NO Not authenticated", tag))
		return nil
	}

	mb, err := s.config.DB.GetOrCreateMailbox(session.user)
	if err != nil {
		writeLine(conn, fmt.Sprintf("%s NO %v", tag, err))
		return nil
	}

	if err := mb.LoadMessages(); err != nil {
		writeLine(conn, fmt.Sprintf("%s NO %v", tag, err))
		return nil
	}

	session.selected = mb
	session.state = IMAPStateSelected

	writeLine(conn, fmt.Sprintf("* %d EXISTS", mb.MessageCount()))
	writeLine(conn, fmt.Sprintf("* %d RECENT", mb.RecentCount()))
	writeLine(conn, fmt.Sprintf("* OK [UIDVALIDITY %d] UIDVALIDITY", mb.UIDValidity))
	writeLine(conn, fmt.Sprintf("* OK [UIDNEXT %d] Predicted next UID", mb.UIDNext))
	writeLine(conn, fmt.Sprintf("* FLAGS (\\Seen \\Answered \\Flagged \\Deleted \\Draft \\Recent)"))
	writeLine(conn, fmt.Sprintf("* OK [PERMANENTFLAGS (\\Seen \\Answered \\Flagged \\Deleted \\Draft)] Limited"))
	writeLine(conn, fmt.Sprintf("%s OK [READ-WRITE] SELECT completed", tag))
	return nil
}

func (s *IMAPServer) cmdExamine(session *IMAPSession, conn net.Conn, tag, arg string) error {
	if session.state != IMAPStateAuth && session.state != IMAPStateSelected {
		writeLine(conn, fmt.Sprintf("%s NO Not authenticated", tag))
		return nil
	}

	mb, err := s.config.DB.GetOrCreateMailbox(session.user)
	if err != nil {
		writeLine(conn, fmt.Sprintf("%s NO %v", tag, err))
		return nil
	}

	if err := mb.LoadMessages(); err != nil {
		writeLine(conn, fmt.Sprintf("%s NO %v", tag, err))
		return nil
	}

	session.selected = mb
	session.state = IMAPStateSelected

	writeLine(conn, fmt.Sprintf("* %d EXISTS", mb.MessageCount()))
	writeLine(conn, fmt.Sprintf("* %d RECENT", mb.RecentCount()))
	writeLine(conn, fmt.Sprintf("* OK [UIDVALIDITY %d] UIDVALIDITY", mb.UIDValidity))
	writeLine(conn, fmt.Sprintf("* OK [UIDNEXT %d] Predicted next UID", mb.UIDNext))
	writeLine(conn, fmt.Sprintf("* FLAGS (\\Seen \\Answered \\Flagged \\Deleted \\Draft \\Recent)"))
	writeLine(conn, fmt.Sprintf("%s OK [READ-ONLY] EXAMINE completed", tag))
	return nil
}

func (s *IMAPServer) cmdFetch(session *IMAPSession, conn net.Conn, tag, arg string, useUID bool) error {
	if session.state != IMAPStateSelected || session.selected == nil {
		writeLine(conn, fmt.Sprintf("%s BAD Not selected", tag))
		return nil
	}

	parts := strings.SplitN(arg, " ", 2)
	if len(parts) < 2 {
		writeLine(conn, fmt.Sprintf("%s BAD FETCH syntax", tag))
		return nil
	}

	seqSet := parts[0]
	msgSet := parts[1]

	items := parseFetchItems(msgSet)
	for seqNum, msg := range session.selected.Messages {
		id := msg.UID
		if !useUID {
			id = uint32(seqNum + 1)
		}
		if matchesSequenceSet(id, seqSet) {
			writeFetchResponse(conn, msg, items, id)
		}
	}

	writeLine(conn, fmt.Sprintf("%s OK FETCH completed", tag))
	return nil
}

func (s *IMAPServer) cmdStore(session *IMAPSession, conn net.Conn, tag, arg string) error {
	if session.state != IMAPStateSelected || session.selected == nil {
		writeLine(conn, fmt.Sprintf("%s BAD Not selected", tag))
		return nil
	}

	parts := strings.SplitN(arg, " ", 3)
	if len(parts) < 3 {
		writeLine(conn, fmt.Sprintf("%s BAD STORE syntax", tag))
		return nil
	}

	seqSet := parts[0]
	item := strings.ToUpper(parts[1])
	value := parts[2]

	msgSet := parseStoreValue(value)
	for _, msg := range session.selected.Messages {
		if matchesSequenceSet(msg.UID, seqSet) {
			switch item {
			case "+FLAGS.SILENT", "+FLAGS":
				for _, flag := range msgSet {
					switch strings.ToUpper(flag) {
					case "\\SEEN":
						msg.Flags |= FlagSeen
					case "\\ANSWERED":
						msg.Flags |= FlagAnswered
					case "\\FLAGGED":
						msg.Flags |= FlagFlagged
					case "\\DELETED":
						msg.Flags |= FlagDeleted
					case "\\DRAFT":
						msg.Flags |= FlagDraft
					}
				}
			case "-FLAGS.SILENT", "-FLAGS":
				for _, flag := range msgSet {
					switch strings.ToUpper(flag) {
					case "\\SEEN":
						msg.Flags &^= FlagSeen
					case "\\ANSWERED":
						msg.Flags &^= FlagAnswered
					case "\\FLAGGED":
						msg.Flags &^= FlagFlagged
					case "\\DELETED":
						msg.Flags &^= FlagDeleted
					case "\\DRAFT":
						msg.Flags &^= FlagDraft
					}
				}
			case "FLAGS.SILENT", "FLAGS":
				for _, flag := range msgSet {
					switch strings.ToUpper(flag) {
					case "\\SEEN":
						msg.Flags |= FlagSeen
					case "\\ANSWERED":
						msg.Flags |= FlagAnswered
					case "\\FLAGGED":
						msg.Flags |= FlagFlagged
					case "\\DELETED":
						msg.Flags |= FlagDeleted
					case "\\DRAFT":
						msg.Flags |= FlagDraft
					}
				}
			}
		}
	}

	writeLine(conn, fmt.Sprintf("%s OK STORE completed", tag))
	return nil
}

func (s *IMAPServer) cmdSearch(session *IMAPSession, conn net.Conn, tag, arg string) error {
	if session.state != IMAPStateSelected || session.selected == nil {
		writeLine(conn, fmt.Sprintf("%s BAD Not selected", tag))
		return nil
	}

	var uids []uint32
	for _, msg := range session.selected.Messages {
		if matchesSearchCriteria(msg, arg) {
			uids = append(uids, msg.UID)
		}
	}

	writeLine(conn, fmt.Sprintf("* SEARCH %s", formatUIDList(uids)))
	writeLine(conn, fmt.Sprintf("%s OK SEARCH completed", tag))
	return nil
}

func (s *IMAPServer) cmdUID(session *IMAPSession, conn net.Conn, tag, arg string) error {
	parts := strings.SplitN(arg, " ", 2)
	if len(parts) < 2 {
		writeLine(conn, fmt.Sprintf("%s BAD UID syntax", tag))
		return nil
	}

	cmd := strings.ToUpper(parts[0])
	rest := parts[1]

	switch cmd {
	case "FETCH":
		return s.cmdFetch(session, conn, tag, rest, true)
	case "STORE":
		return s.cmdStore(session, conn, tag, rest)
	case "SEARCH":
		return s.cmdSearch(session, conn, tag, rest)
	default:
		writeLine(conn, fmt.Sprintf("%s BAD UID command not supported", tag))
		return nil
	}
}

func (s *IMAPServer) cmdIdle(session *IMAPSession, conn net.Conn, tag string) error {
	if session.state != IMAPStateSelected || session.selected == nil {
		writeLine(conn, fmt.Sprintf("%s BAD Not selected", tag))
		return nil
	}

	writeLine(conn, fmt.Sprintf("%s OK IDLE started", tag))
	writeLine(conn, "+ idling")

	for {
		conn.SetReadDeadline(time.Now().Add(29 * time.Minute))
		buf := make([]byte, 1)
		_, err := conn.Read(buf)
		if err != nil {
			conn.SetReadDeadline(time.Time{})
			return nil
		}
		conn.SetReadDeadline(time.Time{})
	}
}

func (s *IMAPServer) cmdList(session *IMAPSession, conn net.Conn, tag, arg string) error {
	writeLine(conn, "* LIST (\\NoInferiors \\Unmarked) \".\" \"INBOX\"")
	writeLine(conn, fmt.Sprintf("%s OK LIST completed", tag))
	return nil
}

func (s *IMAPServer) cmdStatus(session *IMAPSession, conn net.Conn, tag, arg string) error {
	parts := strings.SplitN(arg, " ", 2)
	if len(parts) < 2 {
		writeLine(conn, fmt.Sprintf("%s BAD STATUS syntax", tag))
		return nil
	}

	mailbox := strings.Trim(parts[0], "\"")
	mb, err := s.config.DB.GetOrCreateMailbox(session.user)
	if err != nil {
		writeLine(conn, fmt.Sprintf("%s NO %v", tag, err))
		return nil
	}

	items := strings.Fields(parts[1])
	var responses []string
	for _, item := range items {
		switch strings.ToUpper(item) {
		case "MESSAGES":
			responses = append(responses, fmt.Sprintf("MESSAGES %d", mb.MessageCount()))
		case "RECENT":
			responses = append(responses, fmt.Sprintf("RECENT %d", mb.RecentCount()))
		case "UNSEEN":
			responses = append(responses, fmt.Sprintf("UNSEEN %d", mb.UnseenCount()))
		case "UIDNEXT":
			responses = append(responses, fmt.Sprintf("UIDNEXT %d", mb.UIDNext))
		case "UIDVALIDITY":
			responses = append(responses, fmt.Sprintf("UIDVALIDITY %d", mb.UIDValidity))
		}
	}

	writeLine(conn, fmt.Sprintf("* STATUS \"%s\" (%s)", mailbox, strings.Join(responses, " ")))
	writeLine(conn, fmt.Sprintf("%s OK STATUS completed", tag))
	return nil
}

func matchesSequenceSet(uid uint32, seqSet string) bool {
	seqSet = strings.TrimSpace(seqSet)
	if seqSet == "*" {
		return true
	}

	parts := strings.Split(seqSet, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, ":") {
			rangeParts := strings.SplitN(part, ":", 2)
			if len(rangeParts) == 2 {
				start := parseUID(rangeParts[0])
				end := parseUID(rangeParts[1])
				if start <= uid && (end == 0 || uid <= end) {
					return true
				}
			}
		} else {
			if uid == parseUID(part) {
				return true
			}
		}
	}
	return false
}

func parseUID(s string) uint32 {
	s = strings.TrimSpace(s)
	if s == "*" {
		return 0
	}
	var uid uint32
	fmt.Sscanf(s, "%d", &uid)
	return uid
}

func parseFetchItems(s string) []string {
	s = strings.Trim(s, "()")
	parts := strings.Fields(s)
	for i, part := range parts {
		parts[i] = strings.ToUpper(part)
	}
	return parts
}

func parseStoreValue(s string) []string {
	s = strings.Trim(s, "() ")
	parts := strings.Fields(s)
	if len(parts) > 0 && (parts[0] == "\\FLAGS" || parts[0] == "\\SEEN" || parts[0] == "\\ANSWERED" || parts[0] == "\\FLAGGED" || parts[0] == "\\DELETED" || parts[0] == "\\DRAFT") {
		return parts[1:]
	}
	return parts
}

func matchesSearchCriteria(msg *Message, criteria string) bool {
	criteria = strings.ToLower(criteria)
	if strings.Contains(criteria, "unseen") {
		if msg.Flags&FlagSeen == 0 {
			return true
		}
	}
	if strings.Contains(criteria, "seen") {
		if msg.Flags&FlagSeen != 0 {
			return true
		}
	}
	if strings.Contains(criteria, "flagged") {
		if msg.Flags&FlagFlagged != 0 {
			return true
		}
	}
	if strings.Contains(criteria, "deleted") {
		if msg.Flags&FlagDeleted != 0 {
			return true
		}
	}
	if strings.Contains(criteria, "draft") {
		if msg.Flags&FlagDraft != 0 {
			return true
		}
	}
	if strings.Contains(criteria, "answered") {
		if msg.Flags&FlagAnswered != 0 {
			return true
		}
	}
	if strings.Contains(criteria, "text") {
		idx := strings.Index(criteria, "text")
		if idx >= 0 {
			query := criteria[idx+5:]
			query = strings.Trim(query, "\"")
			if strings.Contains(strings.ToLower(msg.Subject), query) ||
				strings.Contains(strings.ToLower(msg.Body), query) {
				return true
			}
		}
	}
	return false
}

func writeFetchResponse(conn net.Conn, msg *Message, items []string, seqNum uint32) {
	var flags []string
	if msg.Flags&FlagSeen != 0 {
		flags = append(flags, "\\Seen")
	}
	if msg.Flags&FlagAnswered != 0 {
		flags = append(flags, "\\Answered")
	}
	if msg.Flags&FlagFlagged != 0 {
		flags = append(flags, "\\Flagged")
	}
	if msg.Flags&FlagDeleted != 0 {
		flags = append(flags, "\\Deleted")
	}
	if msg.Flags&FlagDraft != 0 {
		flags = append(flags, "\\Draft")
	}
	if msg.Flags&FlagRecent != 0 {
		flags = append(flags, "\\Recent")
	}

	if len(flags) == 0 {
		flags = []string{}
	}

	for _, item := range items {
		switch strings.ToUpper(item) {
		case "UID":
			writeLine(conn, fmt.Sprintf("* %d FETCH (UID %d)", seqNum, msg.UID))
		case "FLAGS":
			writeLine(conn, fmt.Sprintf("* %d FETCH (FLAGS (%s))", seqNum, strings.Join(flags, " ")))
		case "BODY":
			writeLine(conn, fmt.Sprintf("* %d FETCH (BODY \"%s\")", seqNum, msg.String()))
		case "RFC822":
			writeLine(conn, fmt.Sprintf("* %d FETCH (RFC822 %d)", seqNum, msg.Size))
		case "RFC822.SIZE":
			writeLine(conn, fmt.Sprintf("* %d FETCH (RFC822.SIZE %d)", seqNum, msg.Size))
		case "RFC822.HEADER":
			writeLine(conn, fmt.Sprintf("* %d FETCH (RFC822.HEADER {%d\r\n%s\r\n)", seqNum, msg.Size, msg.String()))
		case "INTERNALDATE":
			writeLine(conn, fmt.Sprintf("* %d FETCH (INTERNALDATE \"%s\")", seqNum, msg.Received.Format("02-Jan-2006 15:04:05 -0700")))
		}
	}
}

func formatUIDList(uids []uint32) string {
	if len(uids) == 0 {
		return ""
	}
	var parts []string
	for _, uid := range uids {
		parts = append(parts, fmt.Sprintf("%d", uid))
	}
	return strings.Join(parts, " ")
}

// Stop gracefully shuts down the SMTP server.
func (s *SMTPServer) Stop() {
	s.cancel()
	s.ln.Close()
	s.wg.Wait()
}

// Stop gracefully shuts down the IMAP server.
func (s *IMAPServer) Stop() {
	s.cancel()
	s.ln.Close()
	s.wg.Wait()
}
