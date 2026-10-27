package edgetts

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Communicate handles text-to-speech synthesis via the Edge TTS WebSocket service.
type Communicate struct {
	text   string
	config TTSConfig
	proxy  string

	connectTimeout time.Duration
	receiveTimeout time.Duration
}

// CommunicateOption configures a Communicate instance.
type CommunicateOption func(*Communicate)

// WithProxy sets an HTTP proxy for the WebSocket connection.
func WithProxy(proxy string) CommunicateOption {
	return func(c *Communicate) { c.proxy = proxy }
}

// WithConnectTimeout sets the WebSocket connection timeout.
func WithConnectTimeout(d time.Duration) CommunicateOption {
	return func(c *Communicate) { c.connectTimeout = d }
}

// WithReceiveTimeout sets the timeout for receiving messages.
func WithReceiveTimeout(d time.Duration) CommunicateOption {
	return func(c *Communicate) { c.receiveTimeout = d }
}

// NewCommunicate creates a new Communicate instance for the given text and config.
func NewCommunicate(text string, config TTSConfig, opts ...CommunicateOption) (*Communicate, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	c := &Communicate{
		text:           removeIncompatibleCharacters(text),
		config:         config,
		connectTimeout: 10 * time.Second,
		receiveTimeout: 60 * time.Second,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Stream sends the text to the Edge TTS service and calls the callback for each chunk.
// The callback receives TTSChunk values in order: interleaved audio data and metadata.
func (c *Communicate) Stream(ctx context.Context, callback func(TTSChunk) error) error {
	escaped := xmlEscape(c.text)
	chunks := splitTextByByteLength(escaped, maxTextBytes)
	if len(chunks) == 0 {
		return fmt.Errorf("%w: input text is empty", ErrNoAudioReceived)
	}

	var cumulativeBytes int64
	var offsetCompensation time.Duration

	for _, chunk := range chunks {
		chunkBytes, lastDurationOffset, err := c.streamChunk(ctx, chunk, offsetCompensation, callback)
		if err != nil {
			return err
		}

		cumulativeBytes += chunkBytes
		// Recalculate offset compensation from cumulative bytes for CBR MP3.
		offsetCompensation = bytesToDuration(cumulativeBytes)
		// Also account for the last metadata offset in this chunk.
		_ = lastDurationOffset
	}

	return nil
}

// Save synthesizes the text and writes the audio to a file. If subtitlePath is non-empty,
// it also writes SRT subtitles.
func (c *Communicate) Save(ctx context.Context, audioPath string, subtitlePath string) error {
	audioFile, err := os.Create(audioPath)
	if err != nil {
		return fmt.Errorf("edgetts: creating audio file: %w", err)
	}
	defer audioFile.Close()

	var submaker *SubMaker
	if subtitlePath != "" {
		submaker = NewSubMaker()
	}

	err = c.Stream(ctx, func(chunk TTSChunk) error {
		switch chunk.Type {
		case ChunkTypeAudio:
			if _, err := audioFile.Write(chunk.Data); err != nil {
				return fmt.Errorf("edgetts: writing audio data: %w", err)
			}
		case ChunkTypeWordBoundary, ChunkTypeSentenceBoundary:
			if submaker != nil {
				return submaker.Feed(chunk)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	if submaker != nil && subtitlePath != "" {
		srt := submaker.GetSRT()
		if err := os.WriteFile(subtitlePath, []byte(srt), 0644); err != nil {
			return fmt.Errorf("edgetts: writing subtitle file: %w", err)
		}
	}

	return nil
}

// streamChunk handles a single text chunk over a WebSocket connection.
// It returns the number of audio bytes received and the last metadata duration+offset.
func (c *Communicate) streamChunk(
	ctx context.Context,
	text string,
	offsetCompensation time.Duration,
	callback func(TTSChunk) error,
) (int64, time.Duration, error) {
	conn, err := c.connect(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer conn.Close()

	// Send speech config.
	if err := c.sendSpeechConfig(conn); err != nil {
		return 0, 0, err
	}

	// Send SSML request.
	if err := c.sendSSML(conn, text); err != nil {
		return 0, 0, err
	}

	// Receive responses.
	var chunkAudioBytes int64
	var lastDurationOffset time.Duration
	audioReceived := false

	for {
		if c.receiveTimeout > 0 {
			conn.SetReadDeadline(time.Now().Add(c.receiveTimeout))
		}

		msgType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				break
			}
			return 0, 0, fmt.Errorf("%w: %v", ErrWebSocket, err)
		}

		switch msgType {
		case websocket.TextMessage:
			done, audioBytesFromText, durationOffset, err := c.handleTextMessage(
				string(data), offsetCompensation, callback,
			)
			if err != nil {
				return 0, 0, err
			}
			chunkAudioBytes += audioBytesFromText
			if durationOffset > 0 {
				lastDurationOffset = durationOffset
			}
			if done {
				if !audioReceived {
					return 0, 0, ErrNoAudioReceived
				}
				return chunkAudioBytes, lastDurationOffset, nil
			}

		case websocket.BinaryMessage:
			audioData, err := extractAudioData(data)
			if err != nil {
				return 0, 0, err
			}
			if len(audioData) > 0 {
				audioReceived = true
				chunkAudioBytes += int64(len(audioData))
				if err := callback(TTSChunk{Type: ChunkTypeAudio, Data: audioData}); err != nil {
					return 0, 0, err
				}
			}
		}
	}

	if !audioReceived {
		return 0, 0, ErrNoAudioReceived
	}
	return chunkAudioBytes, lastDurationOffset, nil
}

// synthesizeURL builds the WebSocket URL for a single synthesis attempt.
// The wssURL already carries a TrustedClientToken query string, so the
// per-attempt parameters are appended with "&" instead of "?".
func synthesizeURL() string {
	separator := "?"
	if strings.Contains(wssURL, "?") {
		separator = "&"
	}
	return wssURL + separator +
		"ConnectionId=" + connectID() +
		"&Sec-MS-GEC=" + generateSecMSGEC() +
		"&Sec-MS-GEC-Version=" + secMSGECVersion
}

// connect establishes a WebSocket connection to the Edge TTS service.
func (c *Communicate) connect(ctx context.Context) (*websocket.Conn, error) {
	muid := generateMUID()

	u := synthesizeURL()

	dialer := websocket.Dialer{
		EnableCompression: true,
		HandshakeTimeout:  c.connectTimeout,
	}

	// Record where the TCP connection actually went. A hijacked name or a
	// transparent gateway shows up here as an address that is not Microsoft's,
	// which a status code alone cannot tell.
	var remote string
	forward := &net.Dialer{Timeout: c.connectTimeout}
	dialer.NetDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := forward.DialContext(ctx, network, addr)
		if err == nil {
			remote = conn.RemoteAddr().String()
		}
		return conn, err
	}

	if c.proxy != "" {
		proxyURL, err := url.Parse(c.proxy)
		if err != nil {
			return nil, fmt.Errorf("edgetts: invalid proxy URL: %w", err)
		}
		dialer.Proxy = http.ProxyURL(proxyURL)
	}

	headers := http.Header{
		"Pragma":          {"no-cache"},
		"Cache-Control":   {"no-cache"},
		"Origin":          {"chrome-extension://jdiccldimpdaibmpdkjnbmckianbfold"},
		"User-Agent":      {userAgent},
		"Accept-Encoding": {"gzip, deflate, br, zstd"},
		"Accept-Language": {"en-US,en;q=0.9"},
		"Cookie":          {"muid=" + muid + ";"},
	}

	conn, resp, err := dialer.DialContext(ctx, u, headers)
	if err == nil {
		return conn, nil
	}

	// A rejection that comes with a parseable Date header usually means the
	// token was built from a skewed clock, so correct the skew and try once
	// more with a freshly generated token.
	if resp != nil && resp.StatusCode == http.StatusForbidden {
		if adjErr := adjustClockSkew(resp); adjErr == nil {
			conn, resp, err = dialer.DialContext(ctx, synthesizeURL(), headers)
			if err == nil {
				return conn, nil
			}
		}
	}

	dial := handshakeDial{proxy: c.proxy, remote: remote}
	if resp != nil && c.proxy == "" && remote != "" {
		// Direct dialling, so the certificate the address presents is a fact
		// worth keeping: a middlebox that terminates TLS has to show its own.
		dial.peer = tlsPeerDescription(ctx, remote, c.connectTimeout)
	}
	return nil, handshakeError(err, resp, dial)
}

// tlsPeerDescription reports the certificate and protocol the address at addr
// presents for the service, or an empty string when the probe does not finish.
func tlsPeerDescription(ctx context.Context, addr string, timeout time.Duration) string {
	if timeout <= 0 || timeout > 5*time.Second {
		timeout = 5 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: timeout},
		Config:    &tls.Config{ServerName: host},
	}
	conn, err := dialer.DialContext(probeCtx, "tcp", addr)
	if err != nil {
		return ""
	}
	defer conn.Close()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return ""
	}
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return ""
	}

	issuer := state.PeerCertificates[0].Issuer.CommonName
	if issuer == "" && len(state.PeerCertificates[0].Issuer.Organization) > 0 {
		issuer = state.PeerCertificates[0].Issuer.Organization[0]
	}
	negotiated := state.NegotiatedProtocol
	if negotiated == "" {
		negotiated = "no ALPN"
	}
	if issuer == "" {
		issuer = "unknown issuer"
	}
	return fmt.Sprintf("tls peer %s, issued by %s", negotiated, issuer)
}

// HandshakeError is a refused WebSocket upgrade.
//
// gorilla/websocket reports every refusal as the same bare "websocket: bad
// handshake" while keeping the response readable, so the causes behind an Edge
// TTS rejection — a stale token, a throttled address, a proxy answering instead
// of the service, a middlebox that rewrote the upgrade into a plain GET — are
// indistinguishable without the status and the response headers.
type HandshakeError struct {
	// StatusCode is the status the server answered the upgrade with.
	StatusCode int

	// Detail summarises the response, for the log line.
	Detail string

	// Err is the underlying dial error, normally gorilla's bad handshake.
	Err error
}

func (e *HandshakeError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("edgetts: websocket error: %v (HTTP %d)", e.Err, e.StatusCode)
	}
	return fmt.Sprintf("edgetts: websocket error: %v (HTTP %d: %s)", e.Err, e.StatusCode, e.Detail)
}

// Unwrap reports the failure as a WebSocket error, which is how callers
// recognise a rejected handshake.
func (e *HandshakeError) Unwrap() error { return ErrWebSocket }

// handshakeDial records how a rejected connection was routed, which is what
// separates "the service refused us" from "something answered in its place".
type handshakeDial struct {
	proxy  string // configured proxy, empty when the dial went direct
	remote string // address the TCP connection reached, empty when unknown
	peer   string // certificate the address presented, empty when not probed
}

// handshakeError wraps a dial failure together with what the server answered
// and how the connection was routed.
func handshakeError(err error, resp *http.Response, dial handshakeDial) error {
	if resp == nil {
		return fmt.Errorf("%w: %v", ErrWebSocket, err)
	}
	return &HandshakeError{StatusCode: resp.StatusCode, Detail: responseDetail(resp, dial), Err: err}
}

// responseDetail summarises a handshake response: how the connection was
// routed, whether the upgrade survived the trip, which layer answered, and what
// it said.
func responseDetail(resp *http.Response, dial handshakeDial) string {
	parts := []string{routeDescription(dial)}
	if dial.peer != "" {
		parts = append(parts, dial.peer)
	}

	// A response without the upgrade header was never a WebSocket handshake:
	// the request reached the server as a plain GET, which means a proxy or a
	// firewall in between rewrote it. Microsoft's front door answers such a
	// request with 200 and an empty body.
	if resp.Header.Get("Upgrade") == "" {
		parts = append(parts, "no upgrade header")
	}
	for _, name := range []string{"Server", "X-CDN-TraceID", "Content-Type", "Location"} {
		if value := resp.Header.Get(name); value != "" {
			parts = append(parts, name+": "+value)
		}
	}
	if snippet := responseSnippet(resp); snippet != "" {
		parts = append(parts, "body: "+snippet)
	} else {
		parts = append(parts, "empty body")
	}

	return strings.Join(parts, ", ")
}

// routeDescription names how a rejected connection was routed, with proxy
// credentials stripped, so the log says whether a middlebox was even in play
// and, when dialling direct, which address answered.
func routeDescription(dial handshakeDial) string {
	if dial.proxy != "" {
		if parsed, err := url.Parse(dial.proxy); err == nil && parsed.Host != "" {
			return "via proxy " + parsed.Redacted()
		}
		return "via proxy"
	}
	if dial.remote == "" {
		return "direct"
	}
	return "direct to " + dial.remote
}

// responseSnippet returns the beginning of a handshake response body on a
// single line, or an empty string when there is nothing readable.
func responseSnippet(resp *http.Response) string {
	if resp.Body == nil {
		return ""
	}
	// gorilla/websocket keeps at most the first kibibyte of the body.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	if err != nil {
		return ""
	}

	snippet := strings.Join(strings.Fields(string(body)), " ")
	if runes := []rune(snippet); len(runes) > 200 {
		snippet = string(runes[:200]) + "..."
	}
	return snippet
}

// sendSpeechConfig sends the initial speech configuration message.
func (c *Communicate) sendSpeechConfig(conn *websocket.Conn) error {
	wordBoundary := "false"
	sentenceBoundary := "false"
	if c.config.Boundary == WordBoundary {
		wordBoundary = "true"
	} else {
		sentenceBoundary = "true"
	}

	msg := "X-Timestamp:" + formatTimestamp() + "\r\n" +
		"Content-Type:application/json; charset=utf-8\r\n" +
		"Path:speech.config\r\n\r\n" +
		`{"context":{"synthesis":{"audio":{"metadataoptions":{` +
		`"sentenceBoundaryEnabled":"` + sentenceBoundary + `",` +
		`"wordBoundaryEnabled":"` + wordBoundary + `"},` +
		`"outputFormat":"` + outputFormat + `"}}}}`

	return conn.WriteMessage(websocket.TextMessage, []byte(msg))
}

// sendSSML sends the SSML synthesis request.
func (c *Communicate) sendSSML(conn *websocket.Conn, text string) error {
	ssml := makeSSML(c.config, text)
	msg := "X-RequestId:" + generateMUID() + "\r\n" +
		"Content-Type:application/ssml+xml\r\n" +
		"X-Timestamp:" + formatTimestamp() + "Z\r\n" +
		"Path:ssml\r\n\r\n" +
		ssml

	return conn.WriteMessage(websocket.TextMessage, []byte(msg))
}

// audioMetadata is the JSON structure of audio.metadata messages.
type audioMetadata struct {
	Metadata []metadataEntry `json:"Metadata"`
}

type metadataEntry struct {
	Type string       `json:"Type"`
	Data metadataData `json:"Data"`
}

type metadataData struct {
	Offset   int64        `json:"Offset"`
	Duration int64        `json:"Duration"`
	Text     metadataText `json:"text"`
}

type metadataText struct {
	Text string `json:"Text"`
}

// handleTextMessage processes a text WebSocket message and dispatches metadata chunks.
// Returns (done, audioBytes, lastDurationOffset, error).
func (c *Communicate) handleTextMessage(
	msg string,
	offsetCompensation time.Duration,
	callback func(TTSChunk) error,
) (bool, int64, time.Duration, error) {
	path := extractPath(msg)

	switch path {
	case "turn.end":
		return true, 0, 0, nil

	case "turn.start", "response":
		return false, 0, 0, nil

	case "audio.metadata":
		body := extractBody(msg)
		if body == "" {
			return false, 0, 0, fmt.Errorf("%w: empty audio.metadata body", ErrUnexpectedResponse)
		}

		var meta audioMetadata
		if err := json.Unmarshal([]byte(body), &meta); err != nil {
			return false, 0, 0, fmt.Errorf("%w: invalid audio.metadata JSON: %v", ErrUnexpectedResponse, err)
		}

		var lastDurationOffset time.Duration
		for _, entry := range meta.Metadata {
			switch entry.Type {
			case "WordBoundary", "SentenceBoundary":
				// Convert 100-nanosecond ticks to time.Duration.
				offset := ticksToDuration(entry.Data.Offset) + offsetCompensation
				duration := ticksToDuration(entry.Data.Duration)
				lastDurationOffset = offset + duration

				chunkType := ChunkTypeWordBoundary
				if entry.Type == "SentenceBoundary" {
					chunkType = ChunkTypeSentenceBoundary
				}

				if err := callback(TTSChunk{
					Type:     chunkType,
					Offset:   offset,
					Duration: duration,
					Text:     entry.Data.Text.Text,
				}); err != nil {
					return false, 0, 0, err
				}

			case "SessionEnd":
				// Ignore session end markers.

			default:
				return false, 0, 0, fmt.Errorf("%w: %s", ErrUnknownResponse, entry.Type)
			}
		}
		return false, 0, lastDurationOffset, nil

	default:
		// Ignore unknown paths.
		return false, 0, 0, nil
	}
}

// extractAudioData extracts the audio payload from a binary WebSocket message.
// Binary messages have a 2-byte big-endian header length prefix.
func extractAudioData(data []byte) ([]byte, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("%w: binary message too short", ErrUnexpectedResponse)
	}

	headerLen := int(binary.BigEndian.Uint16(data[:2]))
	dataStart := 2 + headerLen
	if dataStart > len(data) {
		return nil, fmt.Errorf("%w: binary header length exceeds message size", ErrUnexpectedResponse)
	}

	return data[dataStart:], nil
}

// extractPath extracts the Path header value from a WebSocket text message.
func extractPath(msg string) string {
	for line := range strings.SplitSeq(msg, "\r\n") {
		if after, ok := strings.CutPrefix(line, "Path:"); ok {
			return after
		}
	}
	return ""
}

// extractBody extracts the body (after the \r\n\r\n separator) from a WebSocket text message.
func extractBody(msg string) string {
	if _, after, ok := strings.Cut(msg, "\r\n\r\n"); ok {
		return after
	}
	return ""
}

// formatTimestamp returns a timestamp string in the format expected by the service.
func formatTimestamp() string {
	return time.Now().UTC().Format("Mon Jan 02 2006 15:04:05 GMT-0700 (MST)")
}

// ticksToDuration converts 100-nanosecond ticks to a time.Duration.
func ticksToDuration(ticks int64) time.Duration {
	return time.Duration(ticks * 100) // 100ns per tick → nanoseconds
}

// bytesToDuration converts audio byte count to duration for CBR MP3 at the configured bitrate.
func bytesToDuration(bytes int64) time.Duration {
	// ticks = bytes * 8 * ticksPerSecond / audioBitrate
	ticks := bytes * 8 * ticksPerSecond / audioBitrate
	return ticksToDuration(ticks)
}
