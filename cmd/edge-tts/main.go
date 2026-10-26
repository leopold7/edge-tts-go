package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	edgetts "github.com/leopold7/edge-tts-go"
)

func main() {
	var (
		text           string
		file           string
		listVoices     bool
		voice          string
		rate           string
		volume         string
		pitch          string
		writeMedia     string
		writeSubtitles string
		proxy          string
		boundary       string
	)

	flag.StringVar(&text, "t", "", "Text to speak")
	flag.StringVar(&text, "text", "", "Text to speak")
	flag.StringVar(&file, "f", "", "Read text from `file` (use - for stdin)")
	flag.StringVar(&file, "file", "", "Read text from `file` (use - for stdin)")
	flag.BoolVar(&listVoices, "l", false, "List available voices")
	flag.BoolVar(&listVoices, "list-voices", false, "List available voices")
	flag.StringVar(&voice, "v", edgetts.DefaultVoice, "Voice to use")
	flag.StringVar(&voice, "voice", edgetts.DefaultVoice, "Voice to use")
	flag.StringVar(&rate, "rate", "+0%", "Speech rate (e.g. +50%, -30%)")
	flag.StringVar(&volume, "volume", "+0%", "Volume (e.g. +50%, -30%)")
	flag.StringVar(&pitch, "pitch", "+0Hz", "Pitch (e.g. +50Hz, -30Hz)")
	flag.StringVar(&writeMedia, "write-media", "", "Write audio to `file` (use - for stdout)")
	flag.StringVar(&writeSubtitles, "write-subtitles", "", "Write subtitles to `file` (use - for stderr)")
	flag.StringVar(&proxy, "proxy", "", "HTTP proxy URL")
	flag.StringVar(&boundary, "boundary", "sentence", "Boundary type: word or sentence")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if listVoices {
		if err := doListVoices(ctx, proxy); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Determine input text.
	inputText, err := resolveText(text, file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if strings.TrimSpace(inputText) == "" {
		fmt.Fprintf(os.Stderr, "Error: no input text provided. Use -t, -f, or pipe to stdin.\n")
		flag.Usage()
		os.Exit(1)
	}

	boundaryType := edgetts.SentenceBoundary
	if strings.EqualFold(boundary, "word") {
		boundaryType = edgetts.WordBoundary
	}

	config := edgetts.TTSConfig{
		Voice:    voice,
		Rate:     rate,
		Volume:   volume,
		Pitch:    pitch,
		Boundary: boundaryType,
	}

	var opts []edgetts.CommunicateOption
	if proxy != "" {
		opts = append(opts, edgetts.WithProxy(proxy))
	}

	comm, err := edgetts.NewCommunicate(inputText, config, opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := doStream(ctx, comm, writeMedia, writeSubtitles); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func resolveText(text, file string) (string, error) {
	if text != "" {
		return text, nil
	}

	if file != "" {
		var r io.Reader
		if file == "-" || file == "/dev/stdin" {
			r = os.Stdin
		} else {
			f, err := os.Open(file)
			if err != nil {
				return "", fmt.Errorf("opening input file: %w", err)
			}
			defer f.Close()
			r = f
		}

		data, err := io.ReadAll(r)
		if err != nil {
			return "", fmt.Errorf("reading input: %w", err)
		}
		return string(data), nil
	}

	// Check if stdin has data (not a terminal).
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		return string(data), nil
	}

	return "", nil
}

func doListVoices(ctx context.Context, proxy string) error {
	voices, err := edgetts.ListVoices(ctx, proxy)
	if err != nil {
		return err
	}

	for _, v := range voices {
		fmt.Printf("Name: %s\n", v.ShortName)
		fmt.Printf("Gender: %s\n", v.Gender)
		fmt.Printf("Locale: %s\n", v.Locale)
		fmt.Println()
	}
	return nil
}

func doStream(ctx context.Context, comm *edgetts.Communicate, mediaPath, subtitlePath string) error {
	// Determine audio output.
	var audioOut io.Writer
	if mediaPath == "-" {
		audioOut = os.Stdout
	} else if mediaPath != "" {
		f, err := os.Create(mediaPath)
		if err != nil {
			return fmt.Errorf("creating media file: %w", err)
		}
		defer f.Close()
		audioOut = f
	} else {
		// Default: write audio to stdout.
		audioOut = os.Stdout
	}

	// Determine subtitle output.
	var subOut io.Writer
	if subtitlePath == "-" {
		subOut = os.Stderr
	} else if subtitlePath != "" {
		f, err := os.Create(subtitlePath)
		if err != nil {
			return fmt.Errorf("creating subtitle file: %w", err)
		}
		defer f.Close()
		subOut = f
	}

	var submaker *edgetts.SubMaker
	if subOut != nil {
		submaker = edgetts.NewSubMaker()
	}

	err := comm.Stream(ctx, func(chunk edgetts.TTSChunk) error {
		switch chunk.Type {
		case edgetts.ChunkTypeAudio:
			if _, err := audioOut.Write(chunk.Data); err != nil {
				return fmt.Errorf("writing audio: %w", err)
			}
		case edgetts.ChunkTypeWordBoundary, edgetts.ChunkTypeSentenceBoundary:
			if submaker != nil {
				return submaker.Feed(chunk)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	if submaker != nil {
		srt := submaker.GetSRT()
		if _, err := io.WriteString(subOut, srt); err != nil {
			return fmt.Errorf("writing subtitles: %w", err)
		}
	}

	return nil
}
