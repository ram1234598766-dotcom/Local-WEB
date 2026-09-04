package email

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIMAPParser_Capability(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &IMAPConfig{
		Hostname: "127.0.0.1",
		Port:     ln.Addr().(*net.TCPAddr).Port,
		Listener: ln,
		DB:       NewMailboxStore(t.TempDir()),
	}

	server, err := NewIMAPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')

	conn.Write([]byte("A001 CAPABILITY\r\n"))
	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "* CAPABILITY"))
	line, _ = reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "A001 OK"))
}

func TestIMAPParser_Login(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &IMAPConfig{
		Hostname: "127.0.0.1",
		Port:     ln.Addr().(*net.TCPAddr).Port,
		Listener: ln,
		DB:       NewMailboxStore(t.TempDir()),
	}

	server, err := NewIMAPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')

	conn.Write([]byte("A001 LOGIN alice password\r\n"))
	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "A001 OK"))
}

func TestIMAPParser_Select(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &IMAPConfig{
		Hostname: "127.0.0.1",
		Port:     ln.Addr().(*net.TCPAddr).Port,
		Listener: ln,
		DB:       NewMailboxStore(t.TempDir()),
	}

	server, err := NewIMAPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')

	conn.Write([]byte("A001 LOGIN alice password\r\n"))
	reader.ReadString('\n')
	conn.Write([]byte("A002 SELECT INBOX\r\n"))

	var responses []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		responses = append(responses, strings.TrimSpace(line))
		if len(responses) >= 3 && strings.Contains(responses[len(responses)-1], "OK") {
			break
		}
	}

	foundSelect := false
	for _, r := range responses {
		if strings.Contains(r, "EXISTS") {
			foundSelect = true
			break
		}
	}
	assert.True(t, foundSelect, "expected EXISTS response")
}

func TestIMAPParser_Fetch(t *testing.T) {
	dir := t.TempDir()
	store := NewMailboxStore(dir)
	mb, _ := store.GetOrCreateMailbox("alice")
	msg := &Message{
		From:    "bob@example.com",
		To:      []string{"alice@localweb"},
		Subject: "Test",
		Body:    "Body",
	}
	_ = mb.StoreMessage(msg)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &IMAPConfig{
		Hostname: "127.0.0.1",
		Port:     ln.Addr().(*net.TCPAddr).Port,
		Listener: ln,
		DB:       store,
	}

	server, err := NewIMAPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')

	conn.Write([]byte("A001 LOGIN alice password\r\n"))
	reader.ReadString('\n')
	conn.Write([]byte("A002 SELECT INBOX\r\n"))
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "A002 OK") {
			break
		}
	}

	mb2, _ := store.GetOrCreateMailbox("alice")
	for _, m := range mb2.Messages {
		_ = m
	}

	conn.Write([]byte("A003 FETCH 1 (UID FLAGS)\r\n"))
	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "* 1 FETCH"), "got: %q", line)
}

func TestIMAPParser_Store(t *testing.T) {
	dir := t.TempDir()
	store := NewMailboxStore(dir)
	mb, _ := store.GetOrCreateMailbox("alice")
	msg := &Message{
		From:    "bob@example.com",
		To:      []string{"alice@localweb"},
		Subject: "Test",
		Body:    "Body",
	}
	_ = mb.StoreMessage(msg)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &IMAPConfig{
		Hostname: "127.0.0.1",
		Port:     ln.Addr().(*net.TCPAddr).Port,
		Listener: ln,
		DB:       store,
	}

	server, err := NewIMAPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')

	conn.Write([]byte("A001 LOGIN alice password\r\n"))
	reader.ReadString('\n')
	conn.Write([]byte("A002 SELECT INBOX\r\n"))
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "A002 OK") {
			break
		}
	}

	conn.Write([]byte("A003 STORE 1 +FLAGS.SILENT (\\Seen)\r\n"))

	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "A003 OK"))
}

func TestIMAPParser_Search(t *testing.T) {
	dir := t.TempDir()
	store := NewMailboxStore(dir)
	mb, _ := store.GetOrCreateMailbox("alice")
	msg := &Message{
		From:    "bob@example.com",
		To:      []string{"alice@localweb"},
		Subject: "Searchable",
		Body:    "Content",
		Flags:   FlagSeen,
	}
	_ = mb.StoreMessage(msg)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &IMAPConfig{
		Hostname: "127.0.0.1",
		Port:     ln.Addr().(*net.TCPAddr).Port,
		Listener: ln,
		DB:       store,
	}

	server, err := NewIMAPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')

	conn.Write([]byte("A001 LOGIN alice password\r\n"))
	reader.ReadString('\n')
	conn.Write([]byte("A002 SELECT INBOX\r\n"))
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "A002 OK") {
			break
		}
	}
	conn.Write([]byte("A003 SEARCH SEEN\r\n"))

	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "* SEARCH"))
}

func TestIMAPParser_UIDFetch(t *testing.T) {
	dir := t.TempDir()
	store := NewMailboxStore(dir)
	mb, _ := store.GetOrCreateMailbox("alice")
	msg := &Message{
		From:    "bob@example.com",
		To:      []string{"alice@localweb"},
		Subject: "Test",
		Body:    "Body",
	}
	_ = mb.StoreMessage(msg)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &IMAPConfig{
		Hostname: "127.0.0.1",
		Port:     ln.Addr().(*net.TCPAddr).Port,
		Listener: ln,
		DB:       store,
	}

	server, err := NewIMAPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')

	conn.Write([]byte("A001 LOGIN alice password\r\n"))
	reader.ReadString('\n')
	conn.Write([]byte("A002 SELECT INBOX\r\n"))
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "A002 OK") {
			break
		}
	}
	conn.Write([]byte("A003 UID FETCH 2 (UID)\r\n"))

	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "* 2 FETCH"))
}

func TestIMAPParser_Status(t *testing.T) {
	dir := t.TempDir()
	store := NewMailboxStore(dir)
	mb, _ := store.GetOrCreateMailbox("alice")
	msg := &Message{
		From:    "bob@example.com",
		To:      []string{"alice@localweb"},
		Subject: "Test",
		Body:    "Body",
	}
	_ = mb.StoreMessage(msg)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &IMAPConfig{
		Hostname: "127.0.0.1",
		Port:     ln.Addr().(*net.TCPAddr).Port,
		Listener: ln,
		DB:       store,
	}

	server, err := NewIMAPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')

	conn.Write([]byte("A001 LOGIN alice password\r\n"))
	reader.ReadString('\n')
	conn.Write([]byte("A002 STATUS INBOX (MESSAGES RECENT)\r\n"))

	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "* STATUS"))
	line, _ = reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "A002 OK"))
}

func TestIMAPParser_NotAuthenticated(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &IMAPConfig{
		Hostname: "127.0.0.1",
		Port:     ln.Addr().(*net.TCPAddr).Port,
		Listener: ln,
		DB:       NewMailboxStore(t.TempDir()),
	}

	server, err := NewIMAPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')

	conn.Write([]byte("A001 SELECT INBOX\r\n"))
	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "A001 NO"))
}

func TestIMAPParser_Examine(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &IMAPConfig{
		Hostname: "127.0.0.1",
		Port:     ln.Addr().(*net.TCPAddr).Port,
		Listener: ln,
		DB:       NewMailboxStore(t.TempDir()),
	}

	server, err := NewIMAPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')

	conn.Write([]byte("A001 LOGIN alice password\r\n"))
	reader.ReadString('\n')
	conn.Write([]byte("A002 EXAMINE INBOX\r\n"))

	responses := []string{}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		responses = append(responses, strings.TrimSpace(line))
		if strings.Contains(responses[len(responses)-1], "OK") && strings.Contains(responses[len(responses)-1], "EXAMINE") {
			break
		}
	}

	foundReadOnly := false
	for _, r := range responses {
		if strings.Contains(r, "READ-ONLY") {
			foundReadOnly = true
			break
		}
	}
	assert.True(t, foundReadOnly)
}

func TestIMAPParser_Logout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &IMAPConfig{
		Hostname: "127.0.0.1",
		Port:     ln.Addr().(*net.TCPAddr).Port,
		Listener: ln,
		DB:       NewMailboxStore(t.TempDir()),
	}

	server, err := NewIMAPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')

	conn.Write([]byte("A001 LOGOUT\r\n"))
	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "* BYE"))
	line, _ = reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "A001 OK"))
}

func TestIMAPParser_List(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &IMAPConfig{
		Hostname: "127.0.0.1",
		Port:     ln.Addr().(*net.TCPAddr).Port,
		Listener: ln,
		DB:       NewMailboxStore(t.TempDir()),
	}

	server, err := NewIMAPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')

	conn.Write([]byte("A001 LIST \"\" \"*\"\r\n"))
	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "* LIST"))
	line, _ = reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "A001 OK"))
}
