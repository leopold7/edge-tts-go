# edge-tts-go

A Go client for Microsoft Edge's Read Aloud text-to-speech service, usable from
the command line or as a library.

This module keeps working where the upstream Go port does not: Microsoft moved
Read Aloud to the v2 endpoint (`api.msedgeservices.com/tts/cognitiveservices`)
and, in October 2026, to the v2 `Sec-MS-GEC` token, while rejecting the legacy
v1 endpoint and the plain SHA-256 v1 token with HTTP 403 during the WebSocket
handshake. This module speaks only the v2 endpoint and signs every request with
a v2 token. The voice catalog is still served by the legacy v1 host, so
`ListVoices` asks v2 first and falls back to v1.

## Provenance and licence

This is a fork of [`github.com/leopold7/edge-tts-go`](https://github.com/leopold7/edge-tts-go)
`v1.0.1`, itself a Go port of [rany2/edge-tts](https://github.com/rany2/edge-tts).
It is distributed under the same LGPL-3.0 terms; see [LICENSE](LICENSE).

Changes against that base, which is the "modified version" the LGPL asks us to
identify:

- `constants.go`, `communicate.go` — synthesis moved to the v2 endpoint, with the
  client token carried as the `Ocp-Apim-Subscription-Key` query parameter, and
  `Sec-MS-GEC-Version` bumped to the `2-` prefix.
- `gec.go` (new) — generation of the v2 `Sec-MS-GEC` token, a `<16 hex>-<64 hex>`
  pair of a CS64 and a salted SHA-256 digest. A port of `generate_sec_ms_gec_v2`
  from `rany2/edge-tts` 7.3.0 (see rany2/edge-tts#492), verified against a token
  captured from Edge's own traffic.
- `drm.go` — keeps the clock skew learned from a rejected handshake and the MUID;
  the token moved to `gec.go`, and `Date` parsing also accepts a numeric zone
  offset.
- `voices.go` — the voice list is requested from v2 first and falls back to v1.
- `communicate.go` — a refused handshake is reported as a `*HandshakeError`
  carrying the HTTP status, whether the upgrade header survived, and a snippet
  of the body. That is the difference between a stale token, a throttled
  address, a proxy answering instead of the service, and a middlebox that
  rewrote the upgrade into a plain GET (Microsoft answers one with 200 and an
  empty body).

Every change is marked with a `Patched` comment in the source.

## Features

- Text-to-speech synthesis via Microsoft Edge's TTS service
- 400+ voices across 90+ languages
- Adjustable speech rate, volume, and pitch
- SRT subtitle generation with word or sentence boundaries
- Streaming API for real-time audio processing
- HTTP proxy support

## Installation

### CLI

```sh
go install github.com/leopold7/edge-tts-go/cmd/edge-tts@latest
```

### Library

```sh
go get github.com/leopold7/edge-tts-go
```

Requires Go 1.26 or later.

## CLI Usage

### List available voices

```sh
edge-tts --list-voices
```

### Synthesize speech

```sh
# Text to file
edge-tts -t "Hello, world!" --write-media output.mp3

# With a specific voice
edge-tts -t "Bonjour le monde" -v fr-FR-DeniseNeural --write-media output.mp3

# With subtitles
edge-tts -t "Hello, world!" --write-media output.mp3 --write-subtitles output.srt

# From a file
edge-tts -f input.txt --write-media output.mp3

# From stdin
echo "Hello, world!" | edge-tts --write-media output.mp3

# Adjust rate, volume, pitch
edge-tts -t "Slow and low" --rate -30% --volume +50% --pitch -20Hz --write-media output.mp3

# Word-level subtitles
edge-tts -t "Hello, world!" --write-media output.mp3 --write-subtitles output.srt --boundary word

# Via proxy
edge-tts -t "Hello" --proxy http://localhost:8080 --write-media output.mp3
```

### CLI flags

| Flag | Description |
|------|-------------|
| `-t`, `--text` | Text to speak |
| `-f`, `--file` | Read text from file (`-` for stdin) |
| `-l`, `--list-voices` | List available voices |
| `-v`, `--voice` | Voice name (default: `en-US-EmmaMultilingualNeural`) |
| `--rate` | Speech rate, e.g. `+50%`, `-30%` (default: `+0%`) |
| `--volume` | Volume, e.g. `+50%`, `-30%` (default: `+0%`) |
| `--pitch` | Pitch, e.g. `+50Hz`, `-20Hz` (default: `+0Hz`) |
| `--write-media` | Write audio to file (`-` for stdout) |
| `--write-subtitles` | Write SRT subtitles to file (`-` for stderr) |
| `--proxy` | HTTP proxy URL |
| `--boundary` | `word` or `sentence` (default: `sentence`) |

## Library Usage

### Basic synthesis

```go
package main

import (
    "context"
    "log"

    edgetts "github.com/leopold7/edge-tts-go"
)

func main() {
    config := edgetts.DefaultConfig()
    config.Voice = "en-GB-SoniaNeural"

    comm, err := edgetts.NewCommunicate("Hello from Go!", config)
    if err != nil {
        log.Fatal(err)
    }

    err = comm.Save(context.Background(), "output.mp3", "output.srt")
    if err != nil {
        log.Fatal(err)
    }
}
```

### Streaming

```go
err = comm.Stream(ctx, func(chunk edgetts.TTSChunk) error {
    switch chunk.Type {
    case edgetts.ChunkTypeAudio:
        // chunk.Data contains MP3 audio bytes
        player.Write(chunk.Data)
    case edgetts.ChunkTypeWordBoundary:
        // chunk.Text, chunk.Offset, chunk.Duration
        fmt.Printf("[%v] %s\n", chunk.Offset, chunk.Text)
    }
    return nil
})
```

### Listing and filtering voices

```go
vm, err := edgetts.NewVoicesManager(ctx, "")
if err != nil {
    log.Fatal(err)
}

// Find all French female voices
voices := vm.Find(map[string]string{
    "Language": "fr",
    "Gender":   "Female",
})

for _, v := range voices {
    fmt.Println(v.ShortName)
}
```

## Audio format

Output is MP3: 24 kHz, 48 kbps, mono.

## Acknowledgements

This is a Go port of [edge-tts](https://github.com/rany2/edge-tts) by rany2.

## License

This project is licensed under the [GNU Lesser General Public License v3.0](LICENSE), the same license as the original edge-tts project.
