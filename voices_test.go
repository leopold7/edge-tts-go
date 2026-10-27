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

func TestFetchVoiceListParsesCatalog(t *testing.T) {
	calls := 0
	authority := ""
	server := voiceListServer(t, http.StatusOK, `[{"ShortName":"en-US-EmmaMultilingualNeural"}]`, &calls, &authority)

	voices, err := fetchVoiceList(context.Background(), http.DefaultClient, server.URL)
	if err != nil {
		t.Fatalf("fetchVoiceList() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if len(voices) != 1 || voices[0].ShortName != "en-US-EmmaMultilingualNeural" {
		t.Fatalf("voices = %#v, want the parsed catalog", voices)
	}
	if want := strings.TrimPrefix(server.URL, "http://"); authority != want {
		t.Fatalf("Authority = %q, want %q", authority, want)
	}
}

func TestFetchVoiceListReportsStatus(t *testing.T) {
	server := voiceListServer(t, http.StatusForbidden, "", nil, nil)

	_, err := fetchVoiceList(context.Background(), http.DefaultClient, server.URL)
	if err == nil {
		t.Fatal("fetchVoiceList() error = nil, want a failure")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Fatalf("error = %v, want it to mention 403", err)
	}
}

func TestFetchVoiceListStopsWhenContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	server := voiceListServer(t, http.StatusOK, `[]`, &calls, nil)

	if _, err := fetchVoiceList(ctx, http.DefaultClient, server.URL); err == nil {
		t.Fatal("fetchVoiceList() error = nil, want a failure")
	}
	if calls != 0 {
		t.Fatalf("calls = %d, want 0 for a cancelled context", calls)
	}
}
