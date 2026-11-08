package webcdp

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

// webSocketKey is the fixed GUID every RFC 6455 handshake mixes into the accept value.
const webSocketKey = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

const (
	opText   = 0x1
	opBinary = 0x2
	opClose  = 0x8
	opPing   = 0x9
	opPong   = 0xA
)

// wsConn is the minimal RFC 6455 client the DevTools protocol needs: one text frame per message,
// masked client frames, and control frames handled internally. It deliberately avoids a dependency
// so the shared runtime stays standard-library only.
type wsConn struct {
	conn net.Conn
	read *bufio.Reader
	mu   sync.Mutex
}

func acceptKey(key string) string {
	sum := sha1.Sum([]byte(key + webSocketKey))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func dialWS(ctx context.Context, rawURL string) (*wsConn, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "ws" && parsed.Scheme != "wss") {
		return nil, fmt.Errorf("webcdp: unsupported endpoint %q", rawURL)
	}
	address := parsed.Host
	if parsed.Port() == "" {
		address = net.JoinHostPort(parsed.Hostname(), "80")
	}
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(10 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok {
		deadline = contextDeadline
	}
	_ = conn.SetDeadline(deadline)

	keyBytes := make([]byte, 16)
	if _, err = rand.Read(keyBytes); err != nil {
		conn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	request := "GET " + parsed.RequestURI() + " HTTP/1.1\r\n" +
		"Host: " + parsed.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err = io.WriteString(conn, request); err != nil {
		conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !strings.Contains(status, "101") {
		conn.Close()
		return nil, fmt.Errorf("webcdp: handshake rejected: %s", strings.TrimSpace(status))
	}
	headers := map[string]string{}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if index := strings.Index(line, ":"); index > 0 {
			headers[strings.ToLower(strings.TrimSpace(line[:index]))] = strings.TrimSpace(line[index+1:])
		}
	}
	if headers["sec-websocket-accept"] != acceptKey(key) {
		conn.Close()
		return nil, errors.New("webcdp: handshake accept mismatch")
	}
	_ = conn.SetDeadline(time.Time{})
	return newWSConn(conn, reader), nil
}

// newWSConn wraps an established connection; the handshake already consumed its buffered reader.
func newWSConn(conn net.Conn, reader *bufio.Reader) *wsConn {
	if reader == nil {
		reader = bufio.NewReader(conn)
	}
	return &wsConn{conn: conn, read: reader}
}

func (c *wsConn) Close() error { return c.conn.Close() }

func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	header := make([]byte, 0, 14)
	header = append(header, 0x80|opcode)
	size := len(payload)
	switch {
	case size < 126:
		header = append(header, 0x80|byte(size))
	case size <= 0xFFFF:
		header = append(header, 0x80|126, byte(size>>8), byte(size))
	default:
		header = append(header, 0x80|127)
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(size))
		header = append(header, length[:]...)
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	header = append(header, mask[:]...)
	frame := make([]byte, 0, len(header)+size)
	frame = append(frame, header...)
	for index, value := range payload {
		frame = append(frame, value^mask[index%4])
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.conn.Write(frame)
	return err
}

func (c *wsConn) readFrame() (byte, bool, []byte, error) {
	var head [2]byte
	if _, err := io.ReadFull(c.read, head[:]); err != nil {
		return 0, false, nil, err
	}
	final := head[0]&0x80 != 0
	opcode := head[0] & 0x0F
	masked := head[1]&0x80 != 0
	size := uint64(head[1] & 0x7F)
	switch size {
	case 126:
		var extended [2]byte
		if _, err := io.ReadFull(c.read, extended[:]); err != nil {
			return 0, false, nil, err
		}
		size = uint64(binary.BigEndian.Uint16(extended[:]))
	case 127:
		var extended [8]byte
		if _, err := io.ReadFull(c.read, extended[:]); err != nil {
			return 0, false, nil, err
		}
		size = binary.BigEndian.Uint64(extended[:])
	}
	if size > 64<<20 {
		return 0, false, nil, errors.New("webcdp: frame too large")
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.read, mask[:]); err != nil {
			return 0, false, nil, err
		}
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(c.read, payload); err != nil {
		return 0, false, nil, err
	}
	if masked {
		for index := range payload {
			payload[index] ^= mask[index%4]
		}
	}
	return opcode, final, payload, nil
}

func (c *wsConn) readMessage() ([]byte, error) {
	var message []byte
	for {
		opcode, final, payload, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		switch opcode {
		case opText, opBinary:
			message = append(message, payload...)
			if final {
				return message, nil
			}
		case 0x0:
			message = append(message, payload...)
			if final {
				return message, nil
			}
		case opClose:
			return nil, io.EOF
		case opPing:
			if err := c.writeFrame(opPong, payload); err != nil {
				return nil, err
			}
		case opPong:
		default:
			if final {
				return message, nil
			}
		}
	}
}
