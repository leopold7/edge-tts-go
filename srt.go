package edgetts

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Subtitle represents a single subtitle entry.
type Subtitle struct {
	Index int
	Start time.Duration
	End   time.Duration
	Text  string
}

// SubMaker accumulates timing metadata from the TTS stream and generates SRT subtitles.
type SubMaker struct {
	subtitles    []Subtitle
	boundaryType ChunkType
}

// NewSubMaker creates a new SubMaker.
func NewSubMaker() *SubMaker {
	return &SubMaker{}
}

// Feed adds a metadata chunk to the subtitle list.
// All chunks must be the same boundary type (word or sentence).
func (sm *SubMaker) Feed(chunk TTSChunk) error {
	if chunk.Type != ChunkTypeWordBoundary && chunk.Type != ChunkTypeSentenceBoundary {
		return nil // Skip audio chunks.
	}

	if sm.boundaryType == "" {
		sm.boundaryType = chunk.Type
	} else if sm.boundaryType != chunk.Type {
		return ErrMixedBoundaryTypes
	}

	sm.subtitles = append(sm.subtitles, Subtitle{
		Index: len(sm.subtitles) + 1,
		Start: chunk.Offset,
		End:   chunk.Offset + chunk.Duration,
		Text:  chunk.Text,
	})
	return nil
}

// GetSRT generates an SRT formatted string from the accumulated subtitle data.
func (sm *SubMaker) GetSRT() string {
	if len(sm.subtitles) == 0 {
		return ""
	}

	// Sort by start time.
	sorted := make([]Subtitle, len(sm.subtitles))
	copy(sorted, sm.subtitles)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Start != sorted[j].Start {
			return sorted[i].Start < sorted[j].Start
		}
		if sorted[i].End != sorted[j].End {
			return sorted[i].End < sorted[j].End
		}
		return sorted[i].Index < sorted[j].Index
	})

	var b strings.Builder
	idx := 1
	for _, sub := range sorted {
		text := strings.TrimSpace(sub.Text)
		if text == "" {
			continue
		}
		if sub.Start < 0 || sub.Start >= sub.End {
			continue
		}

		fmt.Fprintf(&b, "%d\n", idx)
		fmt.Fprintf(&b, "%s --> %s\n", formatSRTTime(sub.Start), formatSRTTime(sub.End))
		fmt.Fprintf(&b, "%s\n\n", text)
		idx++
	}
	return b.String()
}

// formatSRTTime formats a duration as an SRT timestamp (HH:MM:SS,mmm).
func formatSRTTime(d time.Duration) string {
	totalMs := d.Milliseconds()
	hours := totalMs / 3600000
	totalMs %= 3600000
	minutes := totalMs / 60000
	totalMs %= 60000
	seconds := totalMs / 1000
	ms := totalMs % 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", hours, minutes, seconds, ms)
}
