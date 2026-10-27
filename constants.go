package edgetts

const (
	// trustedClientToken is the token used to authenticate with the Edge TTS service.
	// It is the public Read Aloud client token, carried as a query parameter.
	trustedClientToken = "6A5AA1D4EAFF4E9FB37E23D68491D6F4"

	// chromiumFullVersion is used in User-Agent and GEC version headers.
	// It is kept in step with the value rany2/edge-tts advertises, so the
	// Sec-MS-GEC-Version stamp and the User-Agent never disagree about which
	// browser build the request claims to come from.
	chromiumFullVersion = "143.0.3650.75"

	// chromiumMajorVersion is the major version component.
	chromiumMajorVersion = "143"

	// secMSGECVersion is the Chromium version used for security token generation.
	// Derived from chromiumFullVersion so it cannot drift from the User-Agent.
	// The "2-" prefix selects the CS64 + SHA-256 token format; a "1-" prefix
	// alongside a plain SHA-256 token is rejected with 403 during the
	// WebSocket handshake.
	secMSGECVersion = "2-" + chromiumFullVersion

	// host is the authority that serves the Read Aloud endpoints. It is also
	// the name probed when a refused handshake needs its TLS peer described.
	host = "speech.platform.bing.com"

	// basePath is the Read Aloud service path shared by the WebSocket and the
	// voice list endpoints.
	basePath = "/consumer/speech/synthesize/readaloud"

	// wssURL is the WebSocket URL for the Edge TTS service.
	// It already carries a query string, so communicate.go appends the
	// per-attempt parameters with "&" instead of "?".
	wssURL = "wss://" + host + basePath + "/edge/v1?TrustedClientToken=" + trustedClientToken

	// voiceListURL is the voice list endpoint. Its query parameter is lower
	// case, unlike the WebSocket one.
	voiceListURL = "https://" + host + basePath + "/voices/list?trustedclienttoken=" + trustedClientToken

	// outputFormat is the audio output format requested from the service.
	outputFormat = "audio-24khz-48kbitrate-mono-mp3"

	// userAgent is the User-Agent header sent with requests.
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/" +
		chromiumMajorVersion + ".0.0.0 Safari/537.36 Edg/" + chromiumMajorVersion + ".0.0.0"

	// DefaultVoice is the default voice used for TTS.
	DefaultVoice = "en-US-EmmaMultilingualNeural"

	// maxTextBytes is the maximum number of bytes per text chunk sent to the service.
	maxTextBytes = 4096

	// audioBitrate is the bitrate of the output audio in bits per second.
	audioBitrate = 48000

	// ticksPerSecond is the number of 100-nanosecond ticks per second.
	ticksPerSecond = 10_000_000
)
