package directions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func httpsReference(ref string) (*url.URL, string, error) {
	u, err := url.Parse(ref)
	if err != nil || len(ref) > 4096 || u.Scheme != "https" || u.Hostname() == "" || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery ||
		strings.IndexFunc(ref, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 ||
		strings.ContainsAny(u.Path, "*?[\\") || strings.IndexFunc(u.Path, unicode.IsControl) >= 0 {
		return nil, "", fmt.Errorf("HTTPS references require an absolute URL without credentials, queries, whitespace or globs")
	}
	digest := ""
	if u.Fragment != "" {
		digest = strings.TrimPrefix(u.Fragment, "sha256=")
		if _, err := hex.DecodeString(digest); err != nil || !strings.HasPrefix(u.Fragment, "sha256=") || len(digest) != 64 {
			return nil, "", fmt.Errorf("HTTPS fragments must be #sha256=<64 hex digits>")
		}
	}
	u.Fragment, u.RawFragment = "", ""
	return u, digest, nil
}

func validateReference(ref string) error {
	if strings.HasPrefix(ref, "https://") {
		_, _, err := httpsReference(ref)
		return err
	}
	return validateLocalPath(ref)
}

// sourceReader's authority comes only from host options, never fetched content.
// Its cache lasts for one context build; there is no disk cache.
type sourceReader struct {
	root     *os.Root
	ctx      context.Context
	cancel   context.CancelFunc
	client   *http.Client
	allowed  map[string]bool
	cache    map[string][]byte
	warnings []string
}

func newSourceReader(ctx context.Context, root *os.Root, options ContextOptions) (*sourceReader, error) {
	allowed := map[string]bool{}
	for _, ref := range options.AllowedURLs {
		if _, _, err := httpsReference(ref); err != nil {
			return nil, fmt.Errorf("invalid host-approved URL: %w", err)
		}
		allowed[ref] = true
	}
	transport := options.HTTPTransport
	if transport == nil {
		// No environment proxies, cookies, ambient authentication, or compression.
		transport = &http.Transport{TLSHandshakeTimeout: 5 * time.Second, MaxResponseHeaderBytes: 16 << 10, DisableCompression: true}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	return &sourceReader{root: root, ctx: ctx, cancel: cancel, allowed: allowed, cache: map[string][]byte{},
		client: &http.Client{Transport: transport, Timeout: 5 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func (s *sourceReader) close() {
	s.cancel()
	s.client.CloseIdleConnections()
}

func (s *sourceReader) read(ref string, limit int64) ([]byte, error) {
	if !strings.HasPrefix(ref, "https://") {
		return readRegular(s.root, path.Clean(ref), limit)
	}
	u, digest, err := httpsReference(ref)
	if err != nil {
		return nil, err
	}
	if !s.allowed[ref] {
		return nil, fmt.Errorf("HTTPS source is not host-approved; use --allow-url with the exact reference")
	}
	if data, ok := s.cache[ref]; ok {
		if int64(len(data)) > limit {
			return nil, fmt.Errorf("HTTPS source exceeds size limit")
		}
		return data, nil
	}
	req, err := http.NewRequestWithContext(s.ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("invalid HTTPS request")
	}
	response, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTPS request failed or timed out")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTPS source returned status %d (redirects are not followed)", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(data)) > limit || !utf8.Valid(data) {
		return nil, fmt.Errorf("HTTPS source is unreadable, oversized, or not UTF-8")
	}
	if digest != "" && !strings.EqualFold(digest, fmt.Sprintf("%x", sha256.Sum256(data))) {
		return nil, fmt.Errorf("HTTPS source SHA-256 mismatch")
	}
	if digest == "" && len(s.warnings) == 0 {
		s.warnings = append(s.warnings, "Unpinned HTTPS guidance may change; prefer immutable URLs or #sha256=<digest>.")
	}
	s.cache[ref] = data
	return data, nil
}
