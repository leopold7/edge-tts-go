package edgetts

// This file keeps the pieces of DRM handling that are not the token itself:
// the clock skew learned from a rejected handshake and the machine user ID.
// The token is built in gec.go.

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"
)

// clockSkew tracks the difference between local and server time.
var (
	clockSkew   time.Duration
	clockSkewMu sync.Mutex
)

// generateMUID generates a random Machine User ID (32-character hex string).
func generateMUID() string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(rand.IntN(256))
	}
	return fmt.Sprintf("%X", b)
}

// adjustClockSkew adjusts the local clock skew based on a server response.
// It returns an error if the server date cannot be parsed.
func adjustClockSkew(resp *http.Response) error {
	serverTime, err := parseServerDate(resp.Header.Get("Date"))
	if err != nil {
		return err
	}

	// The Date header carries whole seconds and the response spent a round trip
	// in flight, so the result is off by less than a second. Tokens only change
	// at the five minute window boundary, so the residual is not chased further.
	clockSkewMu.Lock()
	clockSkew = time.Until(serverTime)
	clockSkewMu.Unlock()

	return nil
}

// parseServerDate parses the Date header of a handshake response.
//
// Go's http.ParseTime never consults the process locale, so the strptime bug
// fixed in edge-tts 7.3.1 cannot happen here; what it does not accept is a
// numeric zone offset, which RFC 1123 allows, so that form is tried as well.
func parseServerDate(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("%w: no Date header in response", ErrSkewAdjustment)
	}
	if parsed, err := http.ParseTime(value); err == nil {
		return parsed, nil
	}
	if parsed, err := time.Parse(time.RFC1123Z, value); err == nil {
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("%w: unparsable Date header %q", ErrSkewAdjustment, value)
}
