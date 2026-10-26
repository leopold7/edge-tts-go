package edgetts

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestHandshakeErrorDescribesRejection(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"Content-Type": {"text/plain; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":"Invalid Sec-MS-GEC token"}`)),
	}
	err := handshakeError(errors.New("websocket: bad handshake"), resp, "")

	if !errors.Is(err, ErrWebSocket) {
		t.Fatalf("handshakeError() = %v, want it to wrap ErrWebSocket", err)
	}
	// Callers switch on the status to decide whether another attempt is worth
	// the delay, so it has to survive as a typed field.
	var handshake *HandshakeError
	if !errors.As(err, &handshake) {
		t.Fatalf("handshakeError() = %v, want a *HandshakeError", err)
	}
	if handshake.StatusCode != http.StatusForbidden {
		t.Fatalf("StatusCode = %d, want %d", handshake.StatusCode, http.StatusForbidden)
	}
	for _, want := range []string{"bad handshake", "HTTP 403", "direct", "no upgrade header", "Invalid Sec-MS-GEC token"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("handshakeError() = %v, want it to mention %q", err, want)
		}
	}
}

// TestHandshakeErrorOnPlainHTTPAnswer covers the case seen behind a middlebox
// that rewrites the upgrade into a plain GET: Microsoft's front door answers it
// with 200 and an empty body.
func TestHandshakeErrorOnPlainHTTPAnswer(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}

	err := handshakeError(errors.New("websocket: bad handshake"), resp, "http://user:secret@127.0.0.1:8889")

	for _, want := range []string{"bad handshake", "HTTP 200", "via proxy", "127.0.0.1:8889", "no upgrade header", "empty body"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("handshakeError() = %v, want it to mention %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("handshakeError() = %v, want the proxy password stripped", err)
	}
}

func TestHandshakeErrorWithoutResponse(t *testing.T) {
	err := handshakeError(errors.New("dial tcp: lookup api.msedgeservices.com: no such host"), nil, "")

	if !errors.Is(err, ErrWebSocket) {
		t.Fatalf("handshakeError() = %v, want it to wrap ErrWebSocket", err)
	}
	if strings.Contains(err.Error(), "HTTP") {
		t.Fatalf("handshakeError() = %v, want no status without a response", err)
	}
}

func TestResponseSnippetCollapsesAndTruncates(t *testing.T) {
	body := "  <html>\n  <body>\n" + strings.Repeat("blocked ", 100) + "</body></html>"
	resp := &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader(body))}

	snippet := responseSnippet(resp)
	if strings.ContainsAny(snippet, "\r\n\t") {
		t.Fatalf("responseSnippet() = %q, want a single line", snippet)
	}
	if len([]rune(snippet)) > 203 {
		t.Fatalf("responseSnippet() length = %d, want it truncated", len([]rune(snippet)))
	}
}

func TestParseServerDateAcceptsHTTPDateForms(t *testing.T) {
	want := time.Date(2026, 10, 13, 10, 40, 0, 0, time.UTC)
	for _, value := range []string{
		"Tue, 13 Oct 2026 10:40:00 GMT",
		"Tue, 13 Oct 2026 18:40:00 +0800",
	} {
		got, err := parseServerDate(value)
		if err != nil {
			t.Fatalf("parseServerDate(%q) error = %v", value, err)
		}
		if !got.Equal(want) {
			t.Fatalf("parseServerDate(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestParseServerDateRejectsMissingAndMalformed(t *testing.T) {
	for _, value := range []string{"", "not a date"} {
		if _, err := parseServerDate(value); !errors.Is(err, ErrSkewAdjustment) {
			t.Fatalf("parseServerDate(%q) error = %v, want ErrSkewAdjustment", value, err)
		}
	}
}
