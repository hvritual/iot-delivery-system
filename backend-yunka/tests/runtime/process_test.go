//go:build yu31 && linux

package runtime_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The smoke harness owns and reaps actual executables; it does not use go run,
// bufconn, httptest, fake Principals or in-memory MCP transports.
type child struct {
	cmd      *exec.Cmd
	input    io.WriteCloser
	messages chan json.RawMessage
	done     chan struct{}
	err      error // read only after done closes
	output   lockedBuffer
}
type lockedBuffer struct {
	mu    sync.Mutex
	value bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.value.Len() < 1<<20 {
		_, _ = b.value.Write(p)
	}
	return len(p), nil
}
func (b *lockedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.value.String() }
func executable(t *testing.T, name string) string {
	t.Helper()
	path := os.Getenv(name)
	info, err := os.Stat(path)
	if err != nil || !filepath.IsAbs(path) || info.IsDir() || info.Mode()&0111 == 0 {
		t.Fatalf("%s must identify a built executable; use run-yu31-smoke.sh", name)
	}
	return path
}
func startChild(t *testing.T, ctx context.Context, bin string, env []string, args ...string) *child {
	t.Helper()
	c := &child{done: make(chan struct{}), messages: make(chan json.RawMessage, 64)}
	c.cmd = exec.CommandContext(ctx, bin, args...)
	c.cmd.Env = env
	c.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.cmd.WaitDelay = 3 * time.Second
	var err error
	c.input, err = c.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	c.cmd.Stderr = &c.output
	if err = c.cmd.Start(); err != nil {
		t.Fatalf("start executable: %v", err)
	}
	go func() {
		defer close(c.messages)
		scan := bufio.NewScanner(stdout)
		scan.Buffer(make([]byte, 4096), 1<<20)
		for scan.Scan() {
			line := append(json.RawMessage(nil), scan.Bytes()...)
			select {
			case c.messages <- line:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { c.err = c.cmd.Wait(); close(c.done) }()
	t.Cleanup(func() {
		_ = c.input.Close()
		select {
		case <-c.done:
		default:
			_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
			<-c.done
		}
		// A surviving descendant is an error even when the parent exited cleanly.
		if err := syscall.Kill(-c.cmd.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
			_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
			t.Errorf("process group %d survived cleanup", c.cmd.Process.Pid)
		}
	})
	return c
}
func (c *child) wait(t *testing.T, wantSuccess bool) {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(15 * time.Second):
		t.Fatal("process did not exit within shutdown bound")
	}
	if wantSuccess && c.err != nil {
		t.Fatalf("executable failed: %v (stderr retained privately)", c.err)
	}
	if !wantSuccess && c.err == nil {
		t.Fatal("invalid startup unexpectedly succeeded")
	}
	if err := syscall.Kill(-c.cmd.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatal("process has an unreaped descendant")
	}
	if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(c.cmd.Process.Pid))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("child remains in /proc after Wait")
	}
}
func (c *child) terminate(t *testing.T) {
	t.Helper()
	if err := c.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal("cannot send SIGTERM to running child")
	}
	c.wait(t, true)
	if strings.Contains(c.output.String(), "shutdown iot delivery") || strings.Contains(c.output.String(), "shutdown local delivery") {
		t.Fatal("executable logged a shutdown failure")
	}
}
func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}
func assertReleased(t *testing.T, addresses ...string) {
	t.Helper()
	for _, addr := range addresses {
		l, err := net.Listen("tcp", addr)
		if err != nil {
			t.Fatalf("listener %s was not released", addr)
		}
		_ = l.Close()
	}
}
func cleanEnvironment(values map[string]string) []string {
	env := make([]string, 0, len(os.Environ())+len(values))
	for _, value := range os.Environ() {
		key := strings.SplitN(value, "=", 2)[0]
		if !strings.HasPrefix(key, "IOT_DELIVERY_") && !strings.HasPrefix(key, "OTEL_") {
			env = append(env, value)
		}
	}
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}
func awaitReady(t *testing.T, ctx context.Context, c *child, origin string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-c.done:
			t.Fatal("runtime exited before readiness (stderr retained privately)")
		default:
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/health", nil)
		response, err := (&http.Client{Timeout: time.Second}).Do(req)
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			_ = response.Body.Close()
			if response.StatusCode == 200 && bytes.Contains(body, []byte(`"status":"ok"`)) {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("readiness context expired")
		case <-time.After(30 * time.Millisecond):
		}
	}
	t.Fatal("runtime readiness deadline exceeded")
}

type httpResult struct {
	status  int
	body    []byte
	cookies []*http.Cookie
	header  http.Header
}

func request(t *testing.T, ctx context.Context, origin, method, path, token string, session *login, body any) httpResult {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, origin+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if strings.HasPrefix(path, "/auth/local/") {
		req.Header.Set("Origin", origin)
	}
	if session != nil {
		for _, cookie := range session.cookies {
			req.AddCookie(cookie)
		}
		req.Header.Set("X-CSRF-Token", session.CSRF)
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s transport failed: %v", method, path, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	return httpResult{response.StatusCode, data, response.Cookies(), response.Header.Clone()}
}
func requireStatus(t *testing.T, r httpResult, want int) {
	t.Helper()
	if r.status != want {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(r.body, &e)
		t.Fatalf("HTTP status=%d want=%d category=%s", r.status, want, e.Error)
	}
}
func decode[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return result
}

type mcpClient struct {
	child *child
	id    int
}

func newMCP(t *testing.T, ctx context.Context, env []string) *mcpClient {
	t.Helper()
	c := &mcpClient{child: startChild(t, ctx, executable(t, "YU31_MCP_BIN"), env)}
	raw := c.call(t, ctx, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "yu31-smoke", "version": "1"}})
	result := decode[typeofMCPInit](t, raw)
	if result.Protocol == "" || result.Server.Name != "iot-delivery-system" {
		t.Fatal("MCP initialization did not identify the actual server")
	}
	if err := json.NewEncoder(c.child.input).Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		t.Fatal(err)
	}
	return c
}

type typeofMCPInit struct {
	Protocol string `json:"protocolVersion"`
	Server   struct {
		Name string `json:"name"`
	} `json:"serverInfo"`
}

func (c *mcpClient) call(t *testing.T, ctx context.Context, method string, params any) json.RawMessage {
	t.Helper()
	c.id++
	id := c.id
	if err := json.NewEncoder(c.child.input).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		t.Fatal("write MCP request")
	}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case line, ok := <-c.child.messages:
			if !ok {
				t.Fatal("MCP stdout closed before response")
			}
			var msg struct {
				Version string          `json:"jsonrpc"`
				ID      int             `json:"id"`
				Result  json.RawMessage `json:"result"`
				Error   *struct {
					Code int `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(line, &msg); err != nil || msg.Version != "2.0" {
				t.Fatal("MCP stdout contained non-protocol output")
			}
			if msg.ID == 0 {
				continue
			}
			if msg.ID != id {
				t.Fatal("unexpected MCP response ID")
			}
			if msg.Error != nil {
				t.Fatalf("MCP protocol error code=%d", msg.Error.Code)
			}
			return msg.Result
		case <-timer.C:
			t.Fatal("MCP request timed out")
		case <-ctx.Done():
			t.Fatal("MCP context expired")
		}
	}
}

type toolResult struct {
	IsError    bool            `json:"isError"`
	Structured json.RawMessage `json:"structuredContent"`
	Content    []struct {
		Text string `json:"text"`
	} `json:"content"`
}

func (c *mcpClient) tool(t *testing.T, ctx context.Context, name string, args any, category string) json.RawMessage {
	t.Helper()
	r := decode[toolResult](t, c.call(t, ctx, "tools/call", map[string]any{"name": name, "arguments": args}))
	if category != "" {
		if !r.IsError {
			t.Fatalf("MCP %s accepted a forbidden call", name)
		}
		found := false
		for _, text := range r.Content {
			if text.Text == category {
				found = true
			}
		}
		if !found {
			t.Fatalf("MCP %s did not return exact category %s", name, category)
		}
		return nil
	}
	if r.IsError {
		t.Fatalf("MCP %s returned a tool error", name)
	}
	if len(r.Structured) > 0 {
		return r.Structured
	}
	if len(r.Content) != 1 {
		t.Fatal("MCP missing structured result")
	}
	return []byte(r.Content[0].Text)
}
