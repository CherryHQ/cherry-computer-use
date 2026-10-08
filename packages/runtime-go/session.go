package sdkruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"
)

const cleanupTimeout = time.Second
const queueLimit = 64

type Permission struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Status      string `json:"status"`
	Interaction string `json:"interaction"`
}

type Backend interface {
	Permissions(context.Context) ([]Permission, error)
	Close(context.Context) error
}

// LifecycleBackend has no desktop resources; no script bridge is started.
type LifecycleBackend struct{}

func (LifecycleBackend) Permissions(context.Context) ([]Permission, error) {
	return []Permission{}, nil
}
func (LifecycleBackend) Close(context.Context) error { return nil }

type Config struct {
	SessionID string
	Version   string
	Platform  string
	Backend   Backend
}

func ParseServeArgs(args []string) (string, error) {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	stdio := flags.Bool("stdio", false, "")
	session := flags.String("session-id", "", "")
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	if !*stdio || *session == "" || flags.NArg() != 0 {
		return "", errors.New("usage: serve --stdio --session-id <id>")
	}
	return *session, nil
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *string         `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int               `json:"code"`
	Message string            `json:"message"`
	Data    map[string]string `json:"data"`
}

type response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      string    `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

func failure(id, code, message string) response {
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{
		Code: -32000, Message: message, Data: map[string]string{"code": code, "effect": "none"},
	}}
}

type work struct {
	request
	ctx context.Context
}

func desktopResponse(id string, value any, err error) response {
	if err == nil {
		return response{JSONRPC: "2.0", ID: id, Result: value}
	}
	reason := errorReason(err)
	resp := failure(id, reason.Code, reason.Message)
	var domain *DomainError
	if errors.As(err, &domain) {
		resp.Error.Data["effect"] = domain.Effect
	}
	return resp
}

func (config Config) execute(job work) response {
	id := *job.ID
	if job.ctx.Err() != nil {
		return failure(id, "CANCELLED", "Request cancelled before execution")
	}
	switch job.Method {
	case "getCapabilities", "getPermissionStatus":
		if decodeObject(job.Params, &struct{}{}) != nil {
			return failure(id, "INVALID_ARGUMENT", "Expected empty parameters")
		}
	default:
		if desktop, ok := config.Backend.(*Desktop); ok {
			value, err := desktop.Call(job.ctx, job.Method, job.Params)
			return desktopResponse(id, value, err)
		}
		return failure(id, "UNSUPPORTED_CAPABILITY", "Desktop methods are not connected to the SDK runtime yet")
	}
	if job.Method == "getCapabilities" {
		available := map[string]Availability{}
		if desktop, ok := config.Backend.(*Desktop); ok {
			available = desktop.Capabilities(job.ctx)
		}
		capabilities := make([]any, 0, 9)
		for _, name := range []string{"accessibility", "screenshot", "click", "performSecondaryAction", "scroll", "drag", "typeText", "pressKey", "setValue"} {
			availability, exists := available[name]
			if !exists {
				availability = Availability{Status: "unsupported", Reason: &Reason{"UNSUPPORTED_CAPABILITY", "Capability is not connected to SDK protocol"}}
			}
			capabilities = append(capabilities, map[string]any{"name": name, "availability": availability})
		}
		return response{JSONRPC: "2.0", ID: id, Result: map[string]any{"platform": config.Platform, "capabilities": capabilities}}
	}
	permissions, err := config.Backend.Permissions(job.ctx)
	if job.ctx.Err() != nil {
		return failure(id, "CANCELLED", "Permission query cancelled")
	}
	if err != nil {
		return failure(id, "PERMISSION_REQUIRED", err.Error())
	}
	if permissions == nil {
		permissions = []Permission{}
	}
	return response{JSONRPC: "2.0", ID: id, Result: map[string]any{"permissions": permissions}}
}

// Serve owns the streams until it has settled requests and closed the backend.
func Serve(input io.ReadCloser, output io.WriteCloser, config Config) error {
	if config.SessionID == "" || config.Version == "" || config.Backend == nil {
		return errors.New("incomplete runtime configuration")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer input.Close()
	defer output.Close()
	requests := make(chan request)
	readErr := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(input)
		for {
			body, err := readFrame(reader)
			var req request
			if err == nil {
				err = decodeObject(body, &req)
				var fields map[string]json.RawMessage
				if err == nil {
					_ = json.Unmarshal(body, &fields)
					if _, provided := fields["id"]; provided && req.ID == nil {
						err = errors.New("request ID must be a string")
					}
				}
				if err == nil && (req.JSONRPC != "2.0" || req.Method == "" || (req.ID != nil && *req.ID == "")) {
					err = errors.New("invalid request envelope")
				}
			}
			if err != nil {
				readErr <- err
				return
			}
			select {
			case requests <- req:
			case <-ctx.Done():
				return
			}
		}
	}()
	jobs := make(chan work, queueLimit)
	results := make(chan response, queueLimit)
	go func() {
		for job := range jobs {
			result := config.execute(job)
			select {
			case results <- result:
			case <-ctx.Done():
				return
			}
		}
	}()
	defer close(jobs)
	out := make(chan response, 2*queueLimit)
	writeErr := make(chan error, 1)
	go func() {
		for resp := range out {
			if err := writeFrame(output, resp); err != nil {
				writeErr <- err
				return
			}
		}
		writeErr <- nil
	}()
	send := func(resp response) error {
		select {
		case out <- resp:
			return nil
		default:
			return errors.New("response queue full")
		}
	}
	pending := map[string]context.CancelFunc{}
	initialized := false
	var shutdownID string
	var terminalErr error
	closing := false
	for !closing {
		select {
		case req := <-requests:
			if req.ID == nil {
				var params struct {
					ID string `json:"id"`
				}
				if req.Method != "$/cancelRequest" || decodeObject(req.Params, &params) != nil || params.ID == "" {
					terminalErr, closing = errors.New("invalid notification"), true
				} else if stop := pending[params.ID]; stop != nil {
					stop()
				}
				continue
			}
			id := *req.ID
			if _, exists := pending[id]; exists {
				terminalErr, closing = errors.New("duplicate active request ID"), true
				continue
			}
			var immediate *response
			switch req.Method {
			case "initialize":
				var params struct {
					ProtocolVersion int    `json:"protocolVersion"`
					SessionID       string `json:"sessionId"`
				}
				resp := failure(id, "PROTOCOL_MISMATCH", "Protocol version or private session does not match")
				if !initialized && decodeObject(req.Params, &params) == nil && params.ProtocolVersion == 2 && params.SessionID == config.SessionID {
					initialized = true
					resp = response{JSONRPC: "2.0", ID: id, Result: map[string]any{
						"protocolVersion": 2, "runtimeVersion": config.Version, "sessionId": config.SessionID, "ownership": "private",
					}}
				}
				immediate = &resp
			case "shutdown":
				var params struct {
					SessionID string `json:"sessionId"`
				}
				if decodeObject(req.Params, &params) != nil || params.SessionID != config.SessionID {
					resp := failure(id, "INVALID_ARGUMENT", "Shutdown session does not match")
					immediate = &resp
				} else {
					shutdownID, closing = id, true
				}
			default:
				if !initialized {
					resp := failure(id, "PROTOCOL_ERROR", "Initialize the runtime first")
					immediate = &resp
				} else if len(pending) >= queueLimit && req.Method != "listAppSessions" && (req.Method != "stopAppSession" || len(pending) >= 2*queueLimit) {
					terminalErr, closing = errors.New("request queue full"), true
				} else {
					if desktop, ok := config.Backend.(*Desktop); ok && (req.Method == "stopAppSession" || req.Method == "listAppSessions") {
						if req.Method == "listAppSessions" {
							resp := failure(id, "INVALID_ARGUMENT", "Expected empty parameters")
							if decodeObject(req.Params, &struct{}{}) == nil {
								resp = desktopResponse(id, desktop.listAppSessions(), nil)
							}
							if err := send(resp); err != nil {
								terminalErr, closing = err, true
							}
							continue
						}
						finish, err := desktop.prepareAppStop(req.Params)
						if err != nil {
							if err := send(desktopResponse(id, nil, err)); err != nil {
								terminalErr, closing = err, true
							}
							continue
						}
						pending[id] = func() {}
						go func() {
							value, err := finish()
							select {
							case results <- desktopResponse(id, value, err):
							case <-ctx.Done():
							}
						}()
						continue
					}
					jobCtx, stop := context.WithCancel(ctx)
					pending[id] = stop
					jobs <- work{request: req, ctx: jobCtx}
				}
			}
			if immediate != nil {
				if err := send(*immediate); err != nil {
					terminalErr, closing = err, true
				}
			}
		case resp := <-results:
			pending[resp.ID]()
			delete(pending, resp.ID)
			if err := send(resp); err != nil {
				terminalErr, closing = err, true
			}
		case err := <-readErr:
			if !errors.Is(err, io.EOF) {
				terminalErr = err
			}
			closing = true
		case err := <-writeErr:
			terminalErr, closing = err, true
		}
	}
	for _, stop := range pending {
		stop()
	}
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cleanupCancel()
	for len(pending) > 0 {
		select {
		case resp := <-results:
			delete(pending, resp.ID)
			if shutdownID != "" && terminalErr == nil {
				terminalErr = send(resp)
			}
		case <-cleanupCtx.Done():
			close(out)
			return errors.New("CLEANUP_FAILED: backend request did not stop")
		}
	}
	closed := make(chan error, 1)
	go func() { closed <- config.Backend.Close(cleanupCtx) }()
	select {
	case err := <-closed:
		if err != nil {
			terminalErr = fmt.Errorf("CLEANUP_FAILED: %w", err)
		}
	case <-cleanupCtx.Done():
		terminalErr = errors.New("CLEANUP_FAILED: backend close timed out")
	}
	if shutdownID != "" && terminalErr == nil {
		terminalErr = send(response{JSONRPC: "2.0", ID: shutdownID, Result: map[string]string{"sessionId": config.SessionID, "cleanup": "complete"}})
	}
	close(out)
	if terminalErr != nil {
		return terminalErr
	}
	if shutdownID == "" {
		return nil
	}
	select {
	case err := <-writeErr:
		return err
	case <-time.After(cleanupTimeout):
		return errors.New("protocol output did not drain")
	}
}
