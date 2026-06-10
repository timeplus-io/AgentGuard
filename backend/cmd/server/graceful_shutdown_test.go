package main

import (
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/timeplus/agentguard/internal/holdregistry"
)

func TestShutdownDecisionUsesFailPolicy(t *testing.T) {
	if got := shutdownDecision("allow"); !got.Allow {
		t.Fatalf("allow policy produced %+v, want Allow=true", got)
	}
	if got := shutdownDecision("deny"); got.Allow {
		t.Fatalf("deny policy produced %+v, want Allow=false", got)
	}
	if got := shutdownDecision("unknown"); got.Allow {
		t.Fatalf("unknown policy produced %+v, want fail-closed Allow=false", got)
	}
}

func TestServeWithGracefulShutdownReturnsServeError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	err = serveWithGracefulShutdown(ln, http.NewServeMux(), holdregistry.New(), "deny", make(chan os.Signal))
	if err == nil {
		t.Fatal("expected serve error, got nil")
	}
	if !strings.Contains(err.Error(), "http server:") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServeWithGracefulShutdownDrainsHoldsAndCancelsRequestContexts(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	requestStarted := make(chan struct{})
	requestDone := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-r.Context().Done()
		close(requestDone)
	})

	registry := holdregistry.New()
	holdCh := registry.Register("h1")
	defer registry.Cancel("h1")

	shutdownCh := make(chan os.Signal, 1)
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- serveWithGracefulShutdown(ln, handler, registry, "deny", shutdownCh)
	}()

	clientErr := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		clientErr <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for request to start")
	}

	shutdownCh <- syscall.SIGTERM

	select {
	case got := <-holdCh:
		want := shutdownDecision("deny")
		if got != want {
			t.Fatalf("hold decision = %+v, want %+v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for hold drain")
	}

	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for request context cancellation")
	}

	select {
	case err := <-serverErr:
		if err != nil {
			t.Fatalf("server returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for server shutdown")
	}

	select {
	case <-clientErr:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for client request to finish")
	}
}
