package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/config"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

const testSecret = "0123456789abcdef0123456789abcdef"

// runningServer is a serve() instance listening on a loopback port.
type runningServer struct {
	baseURL string
	addr    string
	stop    context.CancelFunc
	done    chan error
	client  *http.Client
}

// startServer runs serve against dbURL and returns once /v1/readyz answers 200.
// It fails fast if serve exits early instead of waiting for a timeout.
func startServer(t *testing.T, dbURL string) *runningServer {
	t.Helper()
	cfg := config.Config{DatabaseURL: dbURL, WebOrigin: "http://localhost:3000", JWTSecret: testSecret}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	s := &runningServer{
		baseURL: "http://" + ln.Addr().String() + "/v1",
		addr:    ln.Addr().String(),
		stop:    cancel,
		done:    make(chan error, 1),
		client:  &http.Client{Timeout: 2 * time.Second},
	}
	go func() { s.done <- serve(ctx, cfg, slog.New(slog.DiscardHandler), ln) }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case err := <-s.done:
			t.Fatalf("serve exited before it became ready: %v", err)
		default:
		}
		resp, err := s.client.Get(s.baseURL + "/readyz")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return s
			}
			t.Fatalf("GET /readyz = %d, want 200", resp.StatusCode)
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never became reachable: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (s *runningServer) post(t *testing.T, path, body string) (int, []byte) {
	t.Helper()
	resp, err := s.client.Post(s.baseURL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestServeReportsReadyThenShutsDownCleanly(t *testing.T) {
	s := startServer(t, testutil.NewDatabase(t))

	s.stop()

	select {
	case err := <-s.done:
		if err != nil {
			t.Fatalf("serve returned %v after cancel, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return within 5s of cancellation")
	}
	if conn, err := net.DialTimeout("tcp", s.addr, time.Second); err == nil {
		_ = conn.Close()
		t.Error("port still accepts connections after shutdown")
	}
}

// TestServeWiresTheAccountEndpoints proves the real server (config, database,
// service, tokens and router together) can register a user and authenticate
// that user's next request.
func TestServeWiresTheAccountEndpoints(t *testing.T) {
	s := startServer(t, testutil.NewMigratedDatabase(t))

	status, body := s.post(t, "/auth/register", `{"email":"a@example.com","password":"a-long-enough-password","display_name":"A"}`)
	if status != http.StatusCreated {
		t.Fatalf("register: status = %d, body %s", status, body)
	}
	var session struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &session); err != nil || session.AccessToken == "" {
		t.Fatalf("register: no access token in %s (%v)", body, err)
	}

	req, _ := http.NewRequest(http.MethodGet, s.baseURL+"/me", nil)
	req.Header.Set("Authorization", "Bearer "+session.AccessToken)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("GET /me: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /me with the issued token: status = %d, want 200", resp.StatusCode)
	}

	resp, err = s.client.Get(s.baseURL + "/me")
	if err != nil {
		t.Fatalf("GET /me: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /me without a token: status = %d, want 401", resp.StatusCode)
	}
}

func TestServeFailsWhenDatabaseIsUnreachable(t *testing.T) {
	cfg := config.Config{
		DatabaseURL: "postgres://127.0.0.1:1/none?sslmode=disable&connect_timeout=1",
		WebOrigin:   "http://localhost:3000",
		JWTSecret:   testSecret,
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	if err := serve(context.Background(), cfg, slog.New(slog.DiscardHandler), ln); err == nil {
		t.Fatal("serve succeeded against an unreachable database, want error")
	}
}
