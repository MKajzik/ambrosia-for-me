package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/config"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

func TestServeReportsReadyThenShutsDownCleanly(t *testing.T) {
	cfg := config.Config{DatabaseURL: testutil.NewDatabase(t), WebOrigin: "http://localhost:3000"}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, slog.New(slog.DiscardHandler), ln) }()

	url := "http://" + ln.Addr().String() + "/v1/readyz"
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
			t.Fatalf("GET %s = %d, want 200", url, resp.StatusCode)
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never became reachable: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v after cancel, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return within 5s of cancellation")
	}

	if conn, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second); err == nil {
		_ = conn.Close()
		t.Error("port still accepts connections after shutdown")
	}
}

func TestServeFailsWhenDatabaseIsUnreachable(t *testing.T) {
	cfg := config.Config{
		DatabaseURL: "postgres://u:p@127.0.0.1:1/none?sslmode=disable&connect_timeout=1",
		WebOrigin:   "http://localhost:3000",
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
