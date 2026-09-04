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

func TestSMTPParser_EHLO(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &SMTPConfig{
		Hostname:   "127.0.0.1",
		Port:       ln.Addr().(*net.TCPAddr).Port,
		Listener:   ln,
		TLSEnabled: false,
		DB:         NewMailboxStore(t.TempDir()),
		Queue:      NewQueue(),
		PowChecker: NewPoWChecker(4),
	}

	server, err := NewSMTPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// Read greeting
	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "220"))

	// Send EHLO
	conn.Write([]byte("EHLO test.example.com\r\n"))
	line, _ = reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "250-"))
	line, _ = reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "250-"))
	line, _ = reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "250-"))
	line, _ = reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "250"))
}

func TestSMTPParser_HELO(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &SMTPConfig{
		Hostname:   "127.0.0.1",
		Port:       ln.Addr().(*net.TCPAddr).Port,
		Listener:   ln,
		TLSEnabled: false,
		DB:         NewMailboxStore(t.TempDir()),
		Queue:      NewQueue(),
		PowChecker: NewPoWChecker(4),
	}

	server, err := NewSMTPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')

	conn.Write([]byte("HELO test.example.com\r\n"))
	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "250"))
}

func TestSMTPParser_MAILFROM(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &SMTPConfig{
		Hostname:   "127.0.0.1",
		Port:       ln.Addr().(*net.TCPAddr).Port,
		Listener:   ln,
		TLSEnabled: false,
		DB:         NewMailboxStore(t.TempDir()),
		Queue:      NewQueue(),
		PowChecker: NewPoWChecker(4),
	}

	server, err := NewSMTPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')
	conn.Write([]byte("EHLO test.example.com\r\n"))
	for {
		line, _ := reader.ReadString('\n')
		if strings.HasPrefix(line, "250 ") {
			break
		}
	}

	conn.Write([]byte("MAIL FROM:<sender@example.com>\r\n"))
	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "250"))
}

func TestSMTPParser_RCPTTO(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &SMTPConfig{
		Hostname:   "127.0.0.1",
		Port:       ln.Addr().(*net.TCPAddr).Port,
		Listener:   ln,
		TLSEnabled: false,
		DB:         NewMailboxStore(t.TempDir()),
		Queue:      NewQueue(),
		PowChecker: NewPoWChecker(4),
	}

	server, err := NewSMTPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')
	conn.Write([]byte("EHLO test.example.com\r\n"))
	for {
		line, _ := reader.ReadString('\n')
		if strings.HasPrefix(line, "250 ") {
			break
		}
	}

	conn.Write([]byte("MAIL FROM:<sender@example.com>\r\n"))
	reader.ReadString('\n')
	conn.Write([]byte("RCPT TO:<recipient@localweb>\r\n"))
	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "250"))
}

func TestSMTPParser_DATA(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &SMTPConfig{
		Hostname:   "127.0.0.1",
		Port:       ln.Addr().(*net.TCPAddr).Port,
		Listener:   ln,
		TLSEnabled: false,
		DB:         NewMailboxStore(t.TempDir()),
		Queue:      NewQueue(),
		PowChecker: NewPoWChecker(4),
	}

	server, err := NewSMTPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')
	conn.Write([]byte("EHLO test.example.com\r\n"))
	for {
		line, _ := reader.ReadString('\n')
		if strings.HasPrefix(line, "250 ") {
			break
		}
	}

	conn.Write([]byte("MAIL FROM:<sender@example.com>\r\n"))
	reader.ReadString('\n')
	conn.Write([]byte("RCPT TO:<recipient@localweb>\r\n"))
	reader.ReadString('\n')
	conn.Write([]byte("DATA\r\n"))
	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "354"))

	conn.Write([]byte("From: sender@example.com\r\n"))
	conn.Write([]byte("To: recipient@localweb\r\n"))
	conn.Write([]byte("Subject: Test\r\n"))
	conn.Write([]byte("\r\n"))
	conn.Write([]byte("Hello world\r\n"))
	conn.Write([]byte(".\r\n"))

	line, _ = reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "250"))
}

func TestSMTPParser_QUIT(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &SMTPConfig{
		Hostname:   "127.0.0.1",
		Port:       ln.Addr().(*net.TCPAddr).Port,
		Listener:   ln,
		TLSEnabled: false,
		DB:         NewMailboxStore(t.TempDir()),
		Queue:      NewQueue(),
		PowChecker: NewPoWChecker(4),
	}

	server, err := NewSMTPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')
	conn.Write([]byte("QUIT\r\n"))
	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "221"))
}

func TestSMTPParser_Noop(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &SMTPConfig{
		Hostname:   "127.0.0.1",
		Port:       ln.Addr().(*net.TCPAddr).Port,
		Listener:   ln,
		TLSEnabled: false,
		DB:         NewMailboxStore(t.TempDir()),
		Queue:      NewQueue(),
		PowChecker: NewPoWChecker(4),
	}

	server, err := NewSMTPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')
	conn.Write([]byte("NOOP\r\n"))
	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "250"))
}

func TestSMTPParser_Rset(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &SMTPConfig{
		Hostname:   "127.0.0.1",
		Port:       ln.Addr().(*net.TCPAddr).Port,
		Listener:   ln,
		TLSEnabled: false,
		DB:         NewMailboxStore(t.TempDir()),
		Queue:      NewQueue(),
		PowChecker: NewPoWChecker(4),
	}

	server, err := NewSMTPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')
	conn.Write([]byte("EHLO test.example.com\r\n"))
	for {
		line, _ := reader.ReadString('\n')
		if strings.HasPrefix(line, "250 ") {
			break
		}
	}

	conn.Write([]byte("MAIL FROM:<sender@example.com>\r\n"))
	reader.ReadString('\n')
	conn.Write([]byte("RSET\r\n"))
	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "250"))
}

func TestSMTPParser_UnknownCommand(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &SMTPConfig{
		Hostname:   "127.0.0.1",
		Port:       ln.Addr().(*net.TCPAddr).Port,
		Listener:   ln,
		TLSEnabled: false,
		DB:         NewMailboxStore(t.TempDir()),
		Queue:      NewQueue(),
		PowChecker: NewPoWChecker(4),
	}

	server, err := NewSMTPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')
	conn.Write([]byte("BADCMD\r\n"))
	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "500"))
}

func TestSMTPParser_BadSequence(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &SMTPConfig{
		Hostname:   "127.0.0.1",
		Port:       ln.Addr().(*net.TCPAddr).Port,
		Listener:   ln,
		TLSEnabled: false,
		DB:         NewMailboxStore(t.TempDir()),
		Queue:      NewQueue(),
		PowChecker: NewPoWChecker(4),
	}

	server, err := NewSMTPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')
	conn.Write([]byte("DATA\r\n"))
	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "503"))
}

func TestSMTP_MailDelivery(t *testing.T) {
	dir := t.TempDir()
	store := NewMailboxStore(dir)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	config := &SMTPConfig{
		Hostname:   "127.0.0.1",
		Port:       ln.Addr().(*net.TCPAddr).Port,
		Listener:   ln,
		TLSEnabled: false,
		DB:         store,
		Queue:      NewQueue(),
		PowChecker: NewPoWChecker(4),
	}

	server, err := NewSMTPServer(context.Background(), config)
	require.NoError(t, err)
	defer server.Stop()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n')
	conn.Write([]byte("EHLO test.example.com\r\n"))
	for {
		line, _ := reader.ReadString('\n')
		if strings.HasPrefix(line, "250 ") {
			break
		}
	}

	conn.Write([]byte("MAIL FROM:<sender@example.com>\r\n"))
	reader.ReadString('\n')
	conn.Write([]byte("RCPT TO:<alice@localweb>\r\n"))
	reader.ReadString('\n')
	conn.Write([]byte("DATA\r\n"))
	reader.ReadString('\n')
	conn.Write([]byte("From: sender@example.com\r\n"))
	conn.Write([]byte("To: alice@localweb\r\n"))
	conn.Write([]byte("Subject: Test\r\n"))
	conn.Write([]byte("\r\n"))
	conn.Write([]byte("Body\r\n"))
	conn.Write([]byte(".\r\n"))

	line, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(line, "250"))
}
