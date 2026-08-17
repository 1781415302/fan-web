package main

import (
	"net"
	"testing"
)

func TestShouldCleanupRollback(t *testing.T) {
	if !shouldCleanupRollback(8080, 8080) {
		t.Fatal("actual==configured must cleanup rollback assets")
	}
	if shouldCleanupRollback(8081, 8080) {
		t.Fatal("fallback bind must keep .old and .pre-migration.bak")
	}
	if shouldCleanupRollback(0, 8080) {
		t.Fatal("unbound port must not cleanup")
	}
}

func TestPortFlagOverwritesConfiguredPortBeforeListen(t *testing.T) {
	// -port 在 listen 前写入 cfg.Server.Port；清理比较用的是覆盖后的配置端口。
	cfgPort := 8080
	flagPort := 9090
	if flagPort > 0 {
		cfgPort = flagPort
	}
	if !shouldCleanupRollback(9090, cfgPort) {
		t.Fatal("-port overwrite must become the configured port used for cleanup")
	}
	if shouldCleanupRollback(9091, cfgPort) {
		t.Fatal("fallback after -port must still keep rollback assets")
	}
}

func TestListenWithFallbackUsesMaxAttempts(t *testing.T) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	got, actual, err := listenWithFallback(port, 10)
	if err != nil {
		t.Fatalf("listenWithFallback(maxAttempts=10) failed: %v", err)
	}
	defer got.Close()
	if actual == port {
		t.Fatal("expected fallback to a different port when start port is in use")
	}
	if shouldCleanupRollback(actual, port) {
		t.Fatal("fallback bind must not cleanup rollback assets")
	}

	blocker, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	busy := blocker.Addr().(*net.TCPAddr).Port
	if _, _, err := listenWithFallback(busy, 1); err == nil {
		t.Fatal("maxAttempts=1 on a busy port must fail")
	}
}
