package sdkruntime

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const maxFrameBytes = 64 << 20

func readFrame(reader *bufio.Reader) ([]byte, error) {
	length := -1
	headerBytes := 0
	for {
		line, err := reader.ReadSlice('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && (headerBytes != 0 || len(line) != 0) {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		headerBytes += len(line)
		if headerBytes > 8192 || !bytes.HasSuffix(line, []byte("\r\n")) {
			return nil, errors.New("invalid frame header")
		}
		if bytes.Equal(line, []byte("\r\n")) {
			break
		}
		key, value, ok := strings.Cut(string(line[:len(line)-2]), ":")
		if !ok {
			return nil, errors.New("invalid frame header")
		}
		if strings.EqualFold(key, "Content-Length") {
			if length >= 0 {
				return nil, errors.New("duplicate Content-Length")
			}
			value = strings.TrimSpace(value)
			if value == "" || strings.Trim(value, "0123456789") != "" {
				return nil, errors.New("invalid Content-Length")
			}
			length, err = strconv.Atoi(value)
			if err != nil || length <= 0 || length > maxFrameBytes {
				return nil, errors.New("invalid Content-Length")
			}
		}
	}
	if length < 0 {
		return nil, errors.New("missing Content-Length")
	}
	body := make([]byte, length)
	_, err := io.ReadFull(reader, body)
	if errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return body, err
}

func writeFrame(writer io.Writer, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	frame := append([]byte(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))), body...)
	for len(frame) > 0 {
		n, err := writer.Write(frame)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		frame = frame[n:]
	}
	return nil
}

func decodeObject(data []byte, target any) error {
	if len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' {
		return errors.New("expected object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("null protocol fields are not allowed")
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}
