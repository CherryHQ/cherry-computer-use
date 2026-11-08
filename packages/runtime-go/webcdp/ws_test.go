package webcdp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"net"
	"strings"
	"testing"
	"time"
)

func TestAcceptKeyMatchesRFCExample(t *testing.T) {
	// RFC 6455 section 1.3 example.
	if got := acceptKey("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("accept key = %q", got)
	}
}

// tcpPair returns two ends of a buffered local connection, so small writes never block.
func tcpPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	return client, server
}

func TestFrameRoundTrip(t *testing.T) {
	client, server := tcpPair(t)
	defer client.Close()
	defer server.Close()
	writer := newWSConn(client, nil)
	reader := newWSConn(server, nil)

	payloads := [][]byte{
		[]byte(`{"id":1}`),
		bytes.Repeat([]byte("a"), 200),
	}
	for _, payload := range payloads {
		go func(payload []byte) { _ = writer.writeFrame(opText, payload) }(payload)
		_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
		opcode, final, got, err := reader.readFrame()
		if err != nil {
			t.Fatalf("readFrame: %v", err)
		}
		if opcode != opText || !final {
			t.Fatalf("opcode=%d final=%v", opcode, final)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("got %d bytes, want %d", len(got), len(payload))
		}
	}
}

func TestFrameLengthEncoding(t *testing.T) {
	cases := []struct {
		size       int
		headerSize int
	}{
		{10, 2},
		{200, 4},
		{70000, 10},
	}
	for _, testCase := range cases {
		client, server := tcpPair(t)
		writer := newWSConn(client, nil)
		reader := newWSConn(server, nil)
		payload := bytes.Repeat([]byte("x"), testCase.size)
		go func() { _ = writer.writeFrame(opText, payload) }()
		_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, got, err := reader.readFrame()
		if err != nil {
			t.Fatalf("size %d: %v", testCase.size, err)
		}
		if len(got) != testCase.size {
			t.Fatalf("size %d: got %d", testCase.size, len(got))
		}
		client.Close()
		server.Close()
	}
}

func TestReadMessageAnswersPing(t *testing.T) {
	client, server := tcpPair(t)
	defer client.Close()
	defer server.Close()
	reader := newWSConn(client, nil)
	go func() {
		writer := newWSConn(server, nil)
		_ = writer.writeFrame(opPing, []byte("keepalive"))
		_ = writer.writeFrame(opText, []byte("hello"))
	}()
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	message, err := reader.readMessage()
	if err != nil {
		t.Fatalf("readMessage: %v", err)
	}
	if string(message) != "hello" {
		t.Fatalf("message = %q", message)
	}
}

func TestDialHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		request, _ := reader.ReadString('\n')
		key := ""
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			if lower := strings.ToLower(line); strings.HasPrefix(lower, "sec-websocket-key:") {
				key = strings.TrimSpace(line[len("sec-websocket-key:"):])
			}
		}
		_, _ = conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + acceptKey(key) + "\r\n\r\n"))
		accepted <- strings.TrimSpace(request)
	}()
	session, err := Dial(context.Background(), "ws://"+listener.Addr().String()+"/devtools/page/abc")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer session.Close()
	select {
	case request := <-accepted:
		if request != "GET /devtools/page/abc HTTP/1.1" {
			t.Fatalf("request line = %q", request)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handshake was not completed")
	}
}

func TestDialRejectsBadAccept(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if strings.TrimRight(line, "\r\n") == "" {
				break
			}
		}
		_, _ = conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nSec-WebSocket-Accept: " +
			base64.StdEncoding.EncodeToString([]byte("wrong")) + "\r\n\r\n"))
	}()
	if _, err := Dial(context.Background(), "ws://"+listener.Addr().String()+"/devtools/page/abc"); err == nil {
		t.Fatal("expected a handshake error")
	}
}

func TestPortParsing(t *testing.T) {
	cases := []struct {
		line string
		port int
	}{
		{`chrome.exe --remote-debugging-port=9222 --user-data-dir=/tmp/x`, 9222},
		{`"C:\Program Files\App\app.exe" --remote-debugging-port=9333`, 9333},
		{`app.exe --remote-debugging-pipe`, 0},
		{`app.exe`, 0},
		{`app.exe --remote-debugging-port=0`, 0},
		{`app.exe --remote-debugging-port=99999`, 0},
	}
	for _, testCase := range cases {
		if got := portFromCommandLine(testCase.line); got != testCase.port {
			t.Fatalf("portFromCommandLine(%q) = %d, want %d", testCase.line, got, testCase.port)
		}
	}
}

func TestUserDataDirParsing(t *testing.T) {
	cases := []struct {
		line string
		dir  string
	}{
		{`app.exe --user-data-dir="C:\Temp\profile" --remote-debugging-port=1`, `C:\Temp\profile`},
		{`app.exe --user-data-dir=/tmp/profile`, `/tmp/profile`},
		{`app.exe`, ``},
	}
	for _, testCase := range cases {
		if got := userDataDirFromCommandLine(testCase.line); got != testCase.dir {
			t.Fatalf("userDataDirFromCommandLine(%q) = %q, want %q", testCase.line, got, testCase.dir)
		}
	}
}

func TestSelectTarget(t *testing.T) {
	targets := []Target{
		{Type: "page", Title: "Other", WebSocketURL: "ws://other"},
		{Type: "page", Title: "Docs", WebSocketURL: "ws://docs"},
		{Type: "background_page", Title: "Docs", WebSocketURL: "ws://bg"},
	}
	if got := SelectTarget(targets, "Docs - Profile 1 - Microsoft Edge"); got == nil || got.WebSocketURL != "ws://docs" {
		t.Fatalf("prefix match failed: %+v", got)
	}
	if got := SelectTarget(targets, "Docs"); got == nil || got.WebSocketURL != "ws://docs" {
		t.Fatalf("exact match failed: %+v", got)
	}
	if got := SelectTarget(targets, "Unknown"); got == nil || got.WebSocketURL != "ws://other" {
		t.Fatalf("fallback failed: %+v", got)
	}
	if got := SelectTarget([]Target{{Type: "background_page", WebSocketURL: "ws://bg"}}, "x"); got != nil {
		t.Fatalf("expected no page target, got %+v", got)
	}
}

func TestKeyDescriptor(t *testing.T) {
	cases := []struct {
		key          string
		wantKey      string
		wantCode     string
		wantVirtual  int
		wantText     string
		wantModifier int
	}{
		{"Return", "Enter", "Enter", 13, "\r", 0},
		{"backspace", "Backspace", "Backspace", 8, "", 0},
		{"+", "+", "Key+", int('+'), "+", 0},
		{"=", "=", "Key=", int('='), "=", 0},
		{"a", "a", "KeyA", int('A'), "a", 0},
		{"ctrl+shift+s", "s", "KeyS", int('S'), "s", modifierCtrl | modifierShift},
		{"ctrl++", "+", "Key+", int('+'), "+", modifierCtrl},
		{"f12", "f12", "f12", 0x7B, "", 0},
	}
	for _, testCase := range cases {
		spec, ok := keyDescriptor(testCase.key)
		if !ok {
			t.Fatalf("keyDescriptor(%q) rejected the key", testCase.key)
		}
		if spec.Key != testCase.wantKey || spec.Code != testCase.wantCode || spec.VirtualKey != testCase.wantVirtual ||
			spec.Text != testCase.wantText || spec.Modifiers != testCase.wantModifier {
			t.Fatalf("keyDescriptor(%q) = %+v", testCase.key, spec)
		}
	}
	if _, ok := keyDescriptor("ctrl+nonsense"); ok {
		t.Fatal("expected an unsupported key error")
	}
}
