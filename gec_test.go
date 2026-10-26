package edgetts

import (
	"regexp"
	"testing"
	"time"
)

// TestGenerateSecMSGECv2MatchesCapturedToken pins the token generation to a
// value captured from Edge's own Read Aloud traffic for the 10:40Z window on
// 2026-10-13, which is what rany2/edge-tts#490 used to prove the v2 format.
func TestGenerateSecMSGECv2MatchesCapturedToken(t *testing.T) {
	window := time.Date(2026, 10, 13, 10, 40, 0, 0, time.UTC)
	ticks := uint64(window.Unix()+winEpochSeconds) * ticksPerSecond

	const want = "492D7694A183A45E-56E821F95D4579C5B7C7E33DC7303FF379EC36D510213555BBBB9EB17EF15876"
	if got := generateSecMSGECv2(ticks); got != want {
		t.Fatalf("generateSecMSGECv2(%d) = %s, want %s", ticks, got, want)
	}
}

func TestGenerateSecMSGECShape(t *testing.T) {
	// A v1 token was 64 hex digits; a v2 token is a CS64 digest, a hyphen and
	// a SHA-256 digest.
	shape := regexp.MustCompile(`^[0-9A-F]{16}-[0-9A-F]{64}$`)
	if token := generateSecMSGEC(); !shape.MatchString(token) {
		t.Fatalf("generateSecMSGEC() = %q, want <16 hex>-<64 hex>", token)
	}
}

func TestGenerateSecMSGECChangesBetweenWindows(t *testing.T) {
	window := time.Date(2026, 10, 13, 10, 40, 0, 0, time.UTC)
	ticks := uint64(window.Unix()+winEpochSeconds) * ticksPerSecond
	next := ticks + secMSGECWindow*ticksPerSecond

	if generateSecMSGECv2(ticks) == generateSecMSGECv2(next) {
		t.Fatal("adjacent five minute windows produced the same token")
	}
}

func TestSecMSGECTicksRoundDownToWindow(t *testing.T) {
	ticks := secMSGECTicks()
	window := uint64(secMSGECWindow) * ticksPerSecond
	if ticks%window != 0 {
		t.Fatalf("secMSGECTicks() = %d, want a multiple of %d", ticks, window)
	}

	now := uint64(time.Now().Unix()+winEpochSeconds) * ticksPerSecond
	if ticks > now || now-ticks >= window {
		t.Fatalf("secMSGECTicks() = %d, want the window containing %d", ticks, now)
	}
}
