package testutil

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Q2DedPath returns $Q2DED or <repo>/oracle/build/bin/q2ded.
func Q2DedPath() string {
	if v := os.Getenv("Q2DED"); v != "" {
		return v
	}
	root, err := RepoRoot()
	if err != nil {
		return ""
	}
	return filepath.Join(root, "oracle", "build", "bin", "q2ded")
}

// FreeUDPPort returns a currently unused local UDP port.
func FreeUDPPort(t testing.TB) int {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := c.LocalAddr().(*net.UDPAddr).Port
	c.Close()
	return port
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// StartQ2Ded runs the original C dedicated server (oracle/build/bin/q2ded) in
// a temporary directory holding baseq2/pak0.pak (symlink to the demo pak) and
// baseq2/game.so (symlink to oracle/build/bin/baseq2/game.so; Sys_GetGameAPI
// loads <cwd>/<search path>/game.so). The command line is
// "+set dedicated 1 +set port <free port> <args...>", e.g.
// StartQ2Ded(t, "+set", "deathmatch", "1", "+map", "demo1").
// It waits until the server answers an out-of-band "ping" and returns
// "127.0.0.1:<port>". The process is killed on test cleanup. The test is
// skipped when the binary, game.so or the demo pak is missing.
func StartQ2Ded(t testing.TB, args ...string) string {
	t.Helper()
	bin := RequireFile(t, Q2DedPath())
	gameSo := RequireFile(t, filepath.Join(filepath.Dir(bin), "baseq2", "game.so"))
	pak := DemoPak(t)

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "baseq2"), 0o755); err != nil {
		t.Fatal(err)
	}
	for src, dst := range map[string]string{pak: "pak0.pak", gameSo: "game.so"} {
		abs, err := filepath.Abs(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(abs, filepath.Join(dir, "baseq2", dst)); err != nil {
			t.Fatal(err)
		}
	}

	port := FreeUDPPort(t)
	argv := append([]string{"+set", "dedicated", "1", "+set", "port", strconv.Itoa(port)}, args...)
	cmd := exec.Command(bin, argv...)
	cmd.Dir = dir
	out := &lockedBuffer{}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-done
		if t.Failed() {
			t.Logf("q2ded output:\n%s", out.String())
		}
	})

	addr := "127.0.0.1:" + strconv.Itoa(port)
	c, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	buf := make([]byte, 2048)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			t.Fatalf("q2ded exited early:\n%s", out.String())
		default:
		}
		_, _ = c.Write([]byte("\xff\xff\xff\xffping"))
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, err := c.Read(buf)
		if err == nil && n >= 7 && string(buf[4:7]) == "ack" {
			return addr
		}
	}
	t.Fatalf("q2ded did not answer on %s:\n%s", addr, out.String())
	return ""
}
