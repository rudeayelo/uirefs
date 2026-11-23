package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// The test binary re-executes itself as `uirefs serve`; the parent then
// sends SIGTERM and expects a clean exit code 0.
const childEnv = "UIREFS_SIGTERM_TEST_CHILD"

func TestMain(m *testing.M) {
	if os.Getenv(childEnv) == "1" {
		// Child: run the real command path; the exit code is the process's.
		os.Exit(run(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func TestSIGTERMCleanExit(t *testing.T) {
	// A free port on 127.0.0.1.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(), childEnv+"=1")
	attr := &os.ProcAttr{Env: env}
	pid, err := os.StartProcess(executable,
		[]string{executable, "serve", "--port", strconv.Itoa(port), "--data", dir},
		attr)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the API to answer.
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(10 * time.Second)
	up := false
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/api/references")
		if err == nil {
			resp.Body.Close()
			up = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !up {
		pid.Kill()
		t.Fatal("service did not come up")
	}

	// SIGTERM, then expect a clean exit 0 within 5 seconds.
	if err := pid.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan *os.ProcessState)
	go func() {
		state, _ := pid.Wait()
		done <- state
	}()
	select {
	case state := <-done:
		if !state.Success() {
			t.Fatalf("exit code = %d, want 0", state.ExitCode())
		}
	case <-time.After(5 * time.Second):
		pid.Kill()
		t.Fatal("no clean exit within 5 seconds of SIGTERM")
	}
}
