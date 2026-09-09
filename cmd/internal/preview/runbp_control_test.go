package preview

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunbpBarrierWaitsForNativeAcknowledgement(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "runbp-barrier-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "native.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan string, 2)
	go func() {
		for _, response := range []string{"WAIT\n", "OK\n"} {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(client).ReadString('\n')
			received <- line
			client.Write([]byte(response))
			client.Close()
		}
	}()
	if err := runbpBarrier(context.Background(), socket); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if line := <-received; line != "RUNBP_BARRIER\n" {
			t.Fatalf("unexpected request %q", line)
		}
	}
}

func TestRunbpCancelledBarrierDoesNotAcknowledge(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if err := runbpBarrier(ctx, "/nonexistent/socket"); err == nil {
		t.Fatal("cancelled barrier succeeded")
	}
	if time.Since(started) > time.Second {
		t.Fatal("cancellation was ignored")
	}
}

func TestRunbpRequestNeedsIdentityAndRevision(t *testing.T) {
	dir := t.TempDir()
	for _, body := range []string{`{}`, `{"id":"request"}`, `{"revision":"revision"}`, `broken`} {
		if err := os.WriteFile(filepath.Join(dir, "request.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := runbpRead(dir); err == nil {
			t.Fatalf("accepted incomplete request %s", body)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "request.json"), []byte(`{"id":"request","revision":"revision","selector":"Named fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}
	req, err := runbpRead(dir)
	if err != nil || req.ID != "request" || req.Revision != "revision" || req.Selector != "Named fixture" {
		t.Fatalf("request=%+v err=%v", req, err)
	}
}
