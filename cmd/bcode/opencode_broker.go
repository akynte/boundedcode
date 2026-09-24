package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/akynte/boundedcode/internal/session"
	"github.com/akynte/boundedcode/internal/setup"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/supervisor"
)

const brokerEnv = "BC_OPENCODE_BROKER_CAPABILITY"
const brokerCapabilityFile = ".bcode-broker"

type brokerRequest struct {
	Token    string `json:"token"`
	Kind     string `json:"kind"`
	Session  string `json:"session,omitempty"`
	Message  string `json:"message,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Body     string `json:"body,omitempty"`
}
type brokerReply struct {
	Body  string `json:"body,omitempty"`
	Error string `json:"error,omitempty"`
}

// startOpenCodeBroker serves only loopback and requires a random per-run
// bearer capability. The confined process receives neither the ledger path
// nor authority over any sibling workspace.
func startOpenCodeBroker(ctx context.Context, st *store.Store, dataRoot, repo, _, binary string) (string, func(), error) {
	return startOpenCodeBrokerWithRoute(ctx, st, dataRoot, repo, "", binary, "", "")
}

// startOpenCodeBrokerWithRoute is the production broker constructor. The
// allowed route is supplied by bcode opencode run from the Supervisor's
// configured provider/model, not by the worker.
func startOpenCodeBrokerWithRoute(ctx context.Context, st *store.Store, dataRoot, repo, _, binary, allowedProvider, allowedModel string) (string, func(), error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", nil, err
	}
	token := hex.EncodeToString(nonce[:])
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	endpoint := "tcp://" + listener.Addr().String() + "/" + token
	brokerCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	var connections sync.Map
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Store(conn, struct{}{})
			wg.Add(1)
			go func(conn net.Conn) {
				defer wg.Done()
				defer connections.Delete(conn)
				defer conn.Close()
				handleOpenCodeBroker(brokerCtx, conn, st, dataRoot, repo, binary, token, allowedProvider, allowedModel)
			}(conn)
		}
	}()
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			cancel()
			_ = listener.Close()
			connections.Range(func(key, _ any) bool {
				if conn, ok := key.(net.Conn); ok {
					_ = conn.Close()
				}
				return true
			})
			wg.Wait()
		})
	}
	return endpoint, stop, nil
}

func handleOpenCodeBroker(ctx context.Context, conn net.Conn, st *store.Store, dataRoot, repo, binary, token, allowedProvider, allowedModel string) {
	r := bufio.NewReaderSize(conn, 4096)
	var request brokerRequest
	if err := json.NewDecoder(r).Decode(&request); err != nil {
		return
	}
	if subtle.ConstantTimeCompare([]byte(request.Token), []byte(token)) != 1 {
		_ = json.NewEncoder(conn).Encode(brokerReply{Error: "invalid broker capability"})
		return
	}
	reply := brokerReply{}
	switch request.Kind {
	case "authorize":
		if err := supervisor.AuthorizeModelRequestOnRoute(ctx, st, request.Session, request.Provider, request.Model, allowedProvider, allowedModel); err != nil {
			reply.Error = err.Error()
		}
	case "context":
		reply.Body, reply.Error = brokerContext(ctx, st, repo, request.Session)
	case "prompt":
		if len(request.Body) > 1<<20 {
			reply.Error = "prompt exceeds one MiB"
			break
		}
		_, err := supervisor.RecordOpenCodePrompt(ctx, st, request.Session, request.Message, request.Body)
		if err != nil {
			reply.Error = err.Error()
		}
	case "mcp":
		// MCP is served outside the sandbox against only this workspace. The
		// subprocess cannot be redirected to another repository by the client.
		_, _ = io.WriteString(conn, "{\"body\":\"ready\"}\n")
		child := exec.CommandContext(ctx, binary, "--data", dataRoot, "mcp")
		child.Dir = repo
		child.Env = removeEnv(os.Environ(), brokerEnv)
		if os.Getenv("TYPESAFE_API_KEY") == "" {
			if key, keyErr := setup.LoadCredential(dataRoot); keyErr == nil && key != "" {
				child.Env = append(child.Env, "TYPESAFE_API_KEY="+key)
			}
		}
		child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		session.SetParentDeathSignal(child)
		stdin, err := child.StdinPipe()
		if err != nil {
			return
		}
		stdout, err := child.StdoutPipe()
		if err != nil {
			return
		}
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			return
		}
		var copies sync.WaitGroup
		copies.Add(2)
		go func() { defer copies.Done(); _, _ = io.Copy(stdin, r); _ = stdin.Close() }()
		go func() { defer copies.Done(); _, _ = io.Copy(conn, stdout) }()
		copies.Wait()
		_ = child.Wait()
		return
	default:
		reply.Error = "unsupported broker operation"
	}
	_ = json.NewEncoder(conn).Encode(reply)
}

func brokerContext(ctx context.Context, st *store.Store, repo, session string) (string, string) {
	body, err := supervisor.OpenCodeContext(ctx, st, repo, session)
	if err != nil {
		return "", err.Error()
	}
	return body, ""
}

func brokerCall(ctx context.Context, request brokerRequest) (string, error) {
	conn, token, err := brokerDial(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	request.Token = token
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return "", err
	}
	var reply brokerReply
	if err := json.NewDecoder(conn).Decode(&reply); err != nil {
		return "", err
	}
	if reply.Error != "" {
		return "", fmt.Errorf("%s", reply.Error)
	}
	return reply.Body, nil
}

func brokerMCP(ctx context.Context, input io.Reader, output io.Writer) error {
	conn, token, err := brokerDial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(brokerRequest{Kind: "mcp", Token: token}); err != nil {
		return err
	}
	var reply brokerReply
	if err := json.NewDecoder(conn).Decode(&reply); err != nil {
		return err
	}
	if reply.Body != "ready" {
		return fmt.Errorf("broker refused MCP: %s", reply.Error)
	}
	go func() {
		_, _ = io.Copy(conn, input)
		if c, ok := conn.(*net.TCPConn); ok {
			_ = c.CloseWrite()
		}
	}()
	_, err = io.Copy(output, conn)
	return err
}

func brokerSocket() string {
	if path := os.Getenv(brokerEnv); strings.HasPrefix(path, "tcp://127.0.0.1:") {
		return path
	}
	// OpenCode's subprocess driver may filter extension variables. The file
	// lives only in this workspace's private HOME and is removed on exit.
	body, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), brokerCapabilityFile))
	if err != nil {
		return ""
	}
	path := strings.TrimSpace(string(body))
	if strings.HasPrefix(path, "tcp://127.0.0.1:") {
		return path
	}
	return ""
}

func brokerDial(ctx context.Context) (net.Conn, string, error) {
	u, err := url.Parse(brokerSocket())
	if err != nil || u.Scheme != "tcp" || u.Hostname() != "127.0.0.1" || len(strings.TrimPrefix(u.Path, "/")) != 64 {
		return nil, "", fmt.Errorf("invalid broker capability")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, "", err
	}
	return conn, strings.TrimPrefix(u.Path, "/"), nil
}

func removeEnv(env []string, key string) []string {
	out := make([]string, 0, len(env))
	for _, v := range env {
		if !strings.HasPrefix(v, key+"=") {
			out = append(out, v)
		}
	}
	return out
}
