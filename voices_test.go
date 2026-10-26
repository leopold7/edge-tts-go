package edgetts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// voiceListServer serves a canned voice list and records how it was reached.
func voiceListServer(t *testing.T, status int, body string, calls *int, authority *string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			*calls++
		}
		if authority != nil {
			*authority = r.Header.Get("Authority")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestFetchVoiceListWithFallbackPrefersPrimary(t *testing.T) {
	primaryCalls, fallbackCalls := 0, 0
	primary := voiceListServer(t, http.StatusOK, `[{"ShortName":"v2-Voice"}]`, &primaryCalls, nil)
	fallback := voiceListServer(t, http.StatusOK, `[{"ShortName":"v1-Voice"}]`, &fallbackCalls, nil)

	voices, err := fetchVoiceListWithFallback(context.Background(), http.DefaultClient, primary.URL, fallback.URL)
	if err != nil {
		t.Fatalf("fetchVoiceListWithFallback() error = %v", err)
	}
	if primaryCalls != 1 || fallbackCalls != 0 {
		t.Fatalf("calls = primary %d, fallback %d; want 1 and 0", primaryCalls, fallbackCalls)
	}
	if len(voices) != 1 || voices[0].ShortName != "v2-Voice" {
		t.Fatalf("voices = %#v, want the primary catalog", voices)
	}
}

func TestFetchVoiceListWithFallbackUsesLegacyOnRejection(t *testing.T) {
	primaryCalls, fallbackCalls := 0, 0
	fallbackAuthority := ""
	primary := voiceListServer(t, http.StatusForbidden, "", &primaryCalls, nil)
	fallback := voiceListServer(t, http.StatusOK, `[{"ShortName":"v1-Voice"}]`, &fallbackCalls, &fallbackAuthority)

	voices, err := fetchVoiceListWithFallback(context.Background(), http.DefaultClient, primary.URL, fallback.URL)
	if err != nil {
		t.Fatalf("fetchVoiceListWithFallback() error = %v", err)
	}
	if primaryCalls != 1 || fallbackCalls != 1 {
		t.Fatalf("calls = primary %d, fallback %d; want 1 and 1", primaryCalls, fallbackCalls)
	}
	if len(voices) != 1 || voices[0].ShortName != "v1-Voice" {
		t.Fatalf("voices = %#v, want the fallback catalog", voices)
	}
	if want := strings.TrimPrefix(fallback.URL, "http://"); fallbackAuthority != want {
		t.Fatalf("fallback Authority = %q, want %q", fallbackAuthority, want)
	}
}

func TestFetchVoiceListWithFallbackReportsBothFailures(t *testing.T) {
	primary := voiceListServer(t, http.StatusForbidden, "", nil, nil)
	fallback := voiceListServer(t, http.StatusNotFound, "", nil, nil)

	_, err := fetchVoiceListWithFallback(context.Background(), http.DefaultClient, primary.URL, fallback.URL)
	if err == nil {
		t.Fatal("fetchVoiceListWithFallback() error = nil, want a failure")
	}
	for _, want := range []string{"403", "404"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to mention %s", err, want)
		}
	}
}

func TestFetchVoiceListWithFallbackStopsWhenContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	primaryCalls, fallbackCalls := 0, 0
	primary := voiceListServer(t, http.StatusForbidden, "", &primaryCalls, nil)
	fallback := voiceListServer(t, http.StatusOK, `[{"ShortName":"v1-Voice"}]`, &fallbackCalls, nil)

	if _, err := fetchVoiceListWithFallback(ctx, http.DefaultClient, primary.URL, fallback.URL); err == nil {
		t.Fatal("fetchVoiceListWithFallback() error = nil, want a failure")
	}
	if fallbackCalls != 0 {
		t.Fatalf("fallback calls = %d, want 0 for a cancelled context", fallbackCalls)
	}
}
