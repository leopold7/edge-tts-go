package edgetts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ListVoices fetches the list of available voices from the Edge TTS service.
func ListVoices(ctx context.Context, proxy string) ([]Voice, error) {
	client := &http.Client{}
	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("edgetts: invalid proxy URL: %w", err)
		}
		client.Transport = &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	}

	return fetchVoiceList(ctx, client, voiceListURL)
}

// fetchVoiceList performs one voice list request and parses the response.
func fetchVoiceList(ctx context.Context, client *http.Client, listURL string) ([]Voice, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return nil, fmt.Errorf("edgetts: creating voice list request: %w", err)
	}

	authority := ""
	if parsed, parseErr := url.Parse(listURL); parseErr == nil {
		authority = parsed.Host
	}
	req.Header.Set("Authority", authority)
	req.Header.Set("Sec-CH-UA", `"Microsoft Edge";v="`+chromiumMajorVersion+`", "Chromium";v="`+chromiumMajorVersion+`", "Not-A.Brand";v="99"`)
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("edgetts: fetching voice list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("edgetts: voice list returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("edgetts: reading voice list response: %w", err)
	}

	var voices []Voice
	if err := json.Unmarshal(body, &voices); err != nil {
		return nil, fmt.Errorf("edgetts: parsing voice list: %w", err)
	}
	return voices, nil
}

// VoicesManager holds a list of voices and provides filtering methods.
type VoicesManager struct {
	Voices []Voice
}

// NewVoicesManager fetches available voices and returns a VoicesManager.
func NewVoicesManager(ctx context.Context, proxy string) (*VoicesManager, error) {
	voices, err := ListVoices(ctx, proxy)
	if err != nil {
		return nil, err
	}
	return &VoicesManager{Voices: voices}, nil
}

// Find filters voices by the given criteria. All criteria are matched case-insensitively.
// Supported keys: Gender, Locale, Language (prefix match on Locale), Name, ShortName.
func (vm *VoicesManager) Find(criteria map[string]string) []Voice {
	var result []Voice
	for _, v := range vm.Voices {
		if matchVoice(v, criteria) {
			result = append(result, v)
		}
	}
	return result
}

func matchVoice(v Voice, criteria map[string]string) bool {
	for key, want := range criteria {
		want = strings.ToLower(want)
		switch strings.ToLower(key) {
		case "gender":
			if strings.ToLower(v.Gender) != want {
				return false
			}
		case "locale":
			if strings.ToLower(v.Locale) != want {
				return false
			}
		case "language":
			if !strings.HasPrefix(strings.ToLower(v.Locale), want) {
				return false
			}
		case "name":
			if strings.ToLower(v.Name) != want {
				return false
			}
		case "shortname":
			if strings.ToLower(v.ShortName) != want {
				return false
			}
		}
	}
	return true
}
