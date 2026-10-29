package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHTTPSContextCLI(t *testing.T) {
	const prompt = "REMOTE_CLI_CONTEXT"
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, prompt)
	}))
	defer server.Close()
	ref := fmt.Sprintf("%s/context.md#sha256=%x", server.URL, sha256.Sum256([]byte(prompt)))
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".directions"), 0700); err != nil {
		t.Fatal(err)
	}
	config := "meta.version=\"v1.0.0\"\n[repo]\nvcs=\"none\"\n[profiles.default]\nprompt=\"" + ref + "\""
	if err := os.WriteFile(filepath.Join(root, ".directions/base.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if code := run([]string{"context", root}, &out, &errs); code != 1 || !strings.Contains(errs.String(), "host-approved") || requests.Load() != 0 {
		t.Fatal("unapproved URL was not blocked", code, errs.String())
	}
	// Use a fresh CLI process so its normal TLS root store can trust only our
	// loopback fixture's certificate, without changing global state in Go tests.
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".", "context", root, "--allow-url", ref, "--allow-url", "https://example.invalid/unused")
	cmd.Env = append(os.Environ(), "SSL_CERT_FILE="+ca, "SSL_CERT_DIR="+t.TempDir(), "GOPROXY=off", "GOWORK=off", "GOENV=off", "GOTOOLCHAIN=local")
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), prompt) || requests.Load() != 1 {
		t.Fatalf("approved HTTPS CLI context: %v, requests=%d\n%s", err, requests.Load(), output)
	}
}
