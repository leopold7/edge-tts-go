package edgetts

const (
	// trustedClientToken is the token used to authenticate with the Edge TTS service.
	// Patched: synthesis carries it as the Ocp-Apim-Subscription-Key
	// query parameter of the v2 endpoint instead of TrustedClientToken.
	trustedClientToken = "6A5AA1D4EAFF4E9FB37E23D68491D6F4"

	// chromiumFullVersion is used in User-Agent and GEC version headers.
	chromiumFullVersion = "155.0.4283.45"

	// chromiumMajorVersion is the major version component.
	chromiumMajorVersion = "155"

	// secMSGECVersion is the Chromium version used for security token generation.
	// Derived from chromiumFullVersion so it cannot drift from the User-Agent.
	// Patched: the "2-" prefix marks the v2 token format Microsoft
	// started requiring in October 2026; the "1-" prefix alongside a plain
	// SHA-256 token is rejected with 403 during the WebSocket handshake.
	secMSGECVersion = "2-" + chromiumFullVersion

	// host is the authority of the v2 Edge TTS service.
	// Patched: Microsoft migrated Read Aloud to v2 and the backend
	// nodes that enforce it reject legacy v1 WebSocket upgrades with 403.
	host = "api.msedgeservices.com"

	// legacyHost is the authority of the legacy v1 service, which still serves
	// the voice catalog.
	legacyHost = "speech.platform.bing.com"

	// wssURL is the v2 WebSocket URL for the Edge TTS service.
	// It already carries a query string, so communicate.go appends the
	// per-attempt parameters with "&" instead of "?".
	wssURL = "wss://" + host + "/tts/cognitiveservices/websocket/v1?Ocp-Apim-Subscription-Key=" + trustedClientToken

	// voiceListURL is the preferred voice list. Microsoft does not serve it on
	// the v2 host yet (403 whether the subscription key is a query parameter or
	// a request header), so ListVoices falls back to legacyVoiceListURL.
	// Trying v2 first means the catalog switches over on its own once it goes
	// live.
	voiceListURL = "https://" + host + "/tts/cognitiveservices/voices/list?Ocp-Apim-Subscription-Key=" + trustedClientToken

	// legacyVoiceListURL is the v1 voice list, used when voiceListURL fails.
	legacyVoiceListURL = "https://" + legacyHost + "/consumer/speech/synthesize/readaloud/voices/list?trustedclienttoken=" + trustedClientToken

	// outputFormat is the audio output format requested from the service.
	outputFormat = "audio-24khz-48kbitrate-mono-mp3"

	// userAgent is the User-Agent header sent with requests.
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/" +
		chromiumMajorVersion + ".0.0.0 Safari/537.36 Edg/" + chromiumFullVersion

	// DefaultVoice is the default voice used for TTS.
	DefaultVoice = "en-US-EmmaMultilingualNeural"

	// maxTextBytes is the maximum number of bytes per text chunk sent to the service.
	maxTextBytes = 4096

	// audioBitrate is the bitrate of the output audio in bits per second.
	audioBitrate = 48000

	// ticksPerSecond is the number of 100-nanosecond ticks per second.
	ticksPerSecond = 10_000_000
)
