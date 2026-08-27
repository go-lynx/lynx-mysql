package mysql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-lynx/lynx/plugins"
)

func TestDBMysqlClient_HasTrueContextLifecycle(t *testing.T) {
	p := NewMysqlClient()
	if !plugins.HasTrueContextLifecycle(p) {
		t.Fatal("expected plugin to report a true context lifecycle")
	}
	if !plugins.SupportsContextSteps(p) {
		t.Fatal("expected plugin to expose context-aware step hooks")
	}
	// The outer plugin type must be what the framework sees, not a promoted
	// method from an embedded base that would bypass the plugin's own wrapper.
	var _ plugins.ContextStartupTasker = p
	var _ plugins.ContextCleanupTasker = p
	var _ plugins.ContextAwareness = p
	if !p.IsContextAware() {
		t.Fatal("expected IsContextAware() to be true")
	}
}

func TestDBMysqlClient_StartupTasksContext_AlreadyCanceled(t *testing.T) {
	p := NewMysqlClient()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := p.StartupTasksContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("startup did not return promptly: %v", elapsed)
	}
}

func TestDBMysqlClient_CleanupTasksContext_AlreadyCanceled(t *testing.T) {
	p := NewMysqlClient()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := p.CleanupTasksContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestDBMysqlClient_StartupTasksContext_CancelsDuringRetryBackoff(t *testing.T) {
	p := NewMysqlClient()
	p.config.DSN = "root:x@tcp(127.0.0.1:1)/lynx?timeout=200ms"
	p.config.RetryEnabled = true
	p.config.RetryMaxAttempts = 3
	p.config.RetryInitialDelay = 30 // seconds; the deadline must fire during this back-off
	p.config.RetryMaxDelay = 30
	p.config.RetryMultiplier = 1

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := p.StartupTasksContext(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("startup ignored ctx deadline during retry back-off: %v", elapsed)
	}
	if p.IsConnected() {
		t.Fatal("plugin must not be connected after a canceled startup")
	}
}
