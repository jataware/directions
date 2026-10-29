package directions

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func pinned(ref, body string) string {
	return fmt.Sprintf("%s#sha256=%x", ref, sha256.Sum256([]byte(body)))
}

func TestHTTPSReferenceSyntax(t *testing.T) {
	for _, ref := range []string{"https://example.invalid/policy.toml", pinned("https://example.invalid/context.md", "context")} {
		for _, field := range []string{"[profiles.default]\nprompt", "[[overrides]]\npath"} {
			if _, err := Parse([]byte(minimal + field + "=\"" + ref + "\"")); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, ref := range []string{"https://example.invalid/" + strings.Repeat("x", 4096), "http://example.invalid/x", "https://", "https://user@example.invalid/x", "https://example.invalid/x?token=x", "https://example.invalid/x?", "https://example.invalid/*.toml", "https://example.invalid/%2a.toml", "https://example.invalid/x#other", "https://example.invalid/x#sha256=abc", "https://example.invalid/x y", "https://example.invalid/%0a", "file:///x"} {
		if err := validateReference(ref); err == nil {
			t.Fatalf("accepted %q", ref)
		}
	}
	if _, err := Parse([]byte(minimal + "[policies.shared.rule]\ntitle=\"Rule\"\ninstructions=\"Text\"\ntriggers.diff=[\"https://example.invalid/x\"]")); err == nil {
		t.Fatal("URL accepted as a diff glob")
	}
}

func TestApprovedHTTPSSources(t *testing.T) {
	const policy = "[policies.shared.rule]\ninstructions=\"HTTPS_POLICY\"\n"
	const prompt = "HTTPS_CONTEXT"
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != "GET" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.URL.Fragment != "" {
			t.Error("unexpected credentials, method, or fragment")
		}
		if r.URL.Path == "/policy.toml" {
			io.WriteString(w, policy)
		} else {
			io.WriteString(w, prompt)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	policyRef, promptRef := pinned(server.URL+"/policy.toml", policy), pinned(server.URL+"/context.md", prompt)
	config := minimal + "[profiles.default]\nprompt=\"" + promptRef + "\"\n[policies.shared.rule]\ntitle=\"Shared rule\"\ninstructions=\"LOCAL_POLICY\"\n"
	config += "[[overrides]]\npath=\"" + policyRef + "\"\n[[overrides]]\npath=\"" + policyRef + "\"\n"
	put(t, root, ".directions/base.toml", config)
	options := ContextOptions{ExactRoot: true, HTTPTransport: server.Client().Transport}
	if _, err := BuildContext(context.Background(), root, options); err == nil || !strings.Contains(err.Error(), "host-approved") || requests.Load() != 0 {
		t.Fatal("unapproved URL was fetched", err, requests.Load())
	}
	// Approving the same URL without its digest is not equivalent authorization.
	options.AllowedURLs = []string{server.URL + "/policy.toml", promptRef}
	if _, err := BuildContext(context.Background(), root, options); err == nil || requests.Load() != 0 {
		t.Fatal("approval was not exact", err)
	}
	options.AllowedURLs = []string{policyRef, promptRef}
	result, err := BuildContext(context.Background(), root, options)
	if err != nil || !strings.Contains(result.Content, prompt) || !strings.Contains(result.Content, "HTTPS_POLICY") || strings.Contains(result.Content, "LOCAL_POLICY") || len(result.Warnings) != 0 || requests.Load() != 2 {
		t.Fatal(result, err, requests.Load())
	}
	// A fresh build fetches again; unpinned references warn rather than pretending immutability.
	unpinned := server.URL + "/context.md"
	put(t, root, ".directions/base.toml", strings.ReplaceAll(config, promptRef, unpinned))
	options.AllowedURLs = []string{policyRef, unpinned}
	result, err = BuildContext(context.Background(), root, options)
	if err != nil || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "Unpinned HTTPS") || requests.Load() != 4 {
		t.Fatal(result, err, requests.Load())
	}
}

func TestHTTPSBoundaries(t *testing.T) {
	var destinationRequests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/destination", http.StatusFound)
		case "/destination":
			destinationRequests.Add(1)
		case "/large":
			io.WriteString(w, strings.Repeat("x", maxFileBytes+1))
		case "/binary":
			w.Write([]byte{0xff})
		case "/status":
			http.Error(w, "DO_NOT_LEAK_BODY", http.StatusInternalServerError)
		case "/local-ref":
			io.WriteString(w, "[profiles.default]\nprompt=\"secret.md\"")
		case "/remote-ref":
			fmt.Fprintf(w, "[profiles.default]\nprompt=\"https://%s/destination\"", r.Host)
		case "/nested":
			fmt.Fprintf(w, "[[overrides]]\npath=\"https://%s/destination\"", r.Host)
		case "/slow":
			<-r.Context().Done()
		default:
			io.WriteString(w, "[profiles.default]\n")
		}
	}))
	defer server.Close()
	for route, expected := range map[string]string{
		"redirect": "status 302", "large": "oversized", "binary": "not UTF-8", "status": "status 500",
		"local-ref": "remote fragments", "remote-ref": "remote fragments", "nested": "nested overrides",
		"mismatch": "SHA-256 mismatch", "slow": "timed out",
	} {
		t.Run(route, func(t *testing.T) {
			root := t.TempDir()
			put(t, root, "secret.md", "DO_NOT_READ")
			ref := server.URL + "/" + route
			if route == "mismatch" {
				ref += "#sha256=" + strings.Repeat("0", 64)
			}
			put(t, root, ".directions/base.toml", minimal+"[profiles.default]\n[[overrides]]\npath=\""+ref+"\"")
			timeout := 2 * time.Second
			if route == "slow" {
				timeout = 150 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			_, err := BuildContext(ctx, root, ContextOptions{ExactRoot: true, AllowedURLs: []string{ref, server.URL + "/destination"}, HTTPTransport: server.Client().Transport})
			if err == nil || !strings.Contains(err.Error(), expected) || strings.Contains(err.Error(), "DO_NOT_LEAK_BODY") || strings.Contains(err.Error(), "DO_NOT_READ") {
				t.Fatal("unsafe remote source accepted or leaked data", err)
			}
			if destinationRequests.Load() != 0 {
				t.Fatal("followed a redirect or a reference from a remote fragment")
			}
		})
	}
	// Context-size limits still apply to remote prompts, not just override files.
	root := t.TempDir()
	ref := server.URL + "/large"
	put(t, root, ".directions/base.toml", minimal+"[profiles.default]\nprompt=\""+ref+"\"")
	if _, err := BuildContext(context.Background(), root, ContextOptions{ExactRoot: true, AllowedURLs: []string{ref}, HTTPTransport: server.Client().Transport}); err == nil {
		t.Fatal("oversized remote prompt accepted")
	}
}
