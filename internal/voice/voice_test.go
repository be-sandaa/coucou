package voice

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/disgoorg/disgo/voice"
)

// oggFile writes an Ogg Opus file holding packets after the two header packets, all on one page.
func oggFile(t *testing.T, dir, name string, packets ...[]byte) string {
	t.Helper()
	all := append([][]byte{[]byte("OpusHead"), []byte("OpusTags")}, packets...)
	lacing := make([]byte, 0, len(all))
	var data []byte
	for _, p := range all {
		n, ok := segmentCount(len(p))
		if !ok || n == 255 {
			t.Fatalf("packet of %d bytes needs more than one lacing value", len(p))
		}
		lacing = append(lacing, n)
		data = append(data, p...)
	}
	path := filepath.Join(dir, name+".ogg")
	if err := os.WriteFile(path, oggPage(t, 0, lacing, data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// frames pulls n frames from s, naming silence "S" and each packet by its bytes.
func frames(t *testing.T, s *stream, n int) []string {
	t.Helper()
	out := make([]string, n)
	for i := range out {
		b, err := s.ProvideOpusFrame()
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		out[i] = string(b)
		if bytes.Equal(b, voice.SilenceAudioFrame) {
			out[i] = "S"
		}
	}
	return out
}

func loaded(t *testing.T, clips ...Clip) *stream {
	t.Helper()
	s, err := load(clips)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return s
}

// A gap is whole 20ms silence frames, rounded up so it is never shorter than asked; 0 is back to back.
func TestStreamGaps(t *testing.T) {
	const a1, a2, b1, S = "a1", "a2", "b1", "S"
	dir := t.TempDir()
	a := oggFile(t, dir, "a", []byte(a1), []byte(a2))
	b := oggFile(t, dir, "b", []byte(b1))
	tests := []struct {
		name string
		gap  time.Duration
		want []string
	}{
		{"60ms is three frames", 60 * time.Millisecond, []string{a1, a2, S, S, S, b1, S, S}},
		{"50ms rounds up to three", 50 * time.Millisecond, []string{a1, a2, S, S, S, b1, S, S}},
		{"0 is back to back", 0, []string{a1, a2, b1, S, S, S, S, S}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := loaded(t, Clip{File: a, Gap: time.Second}, Clip{File: b, Gap: tt.gap}) // the first gap is suspense's
			if got := frames(t, s, len(tt.want)); !slices.Equal(got, tt.want) {
				t.Errorf("frames = %v, want %v", got, tt.want)
			}
		})
	}
}

// ended carries each clip's index as its last packet is handed out.
func TestStreamSignalsEachClipsEnd(t *testing.T) {
	dir := t.TempDir()
	s := loaded(t, Clip{File: oggFile(t, dir, "a", []byte("a1"), []byte("a2"))}, Clip{File: oggFile(t, dir, "b", []byte("b1")), Gap: 20 * time.Millisecond})
	frames(t, s, 1)
	if len(s.ended) != 0 {
		t.Fatal("ended before its last packet")
	}
	frames(t, s, 1)
	if i := <-s.ended; i != 0 {
		t.Fatalf("ended %d, want 0", i)
	}
	frames(t, s, 2)
	if i := <-s.ended; i != 1 {
		t.Fatalf("ended %d, want 1", i)
	}
}

// Past the last clip it plays silence, never an empty frame: disgo v0.19.6 answers an empty frame
// with SetSpeaking(None), which deadlocks against conn.Close and wedges gateway dispatch.
func TestStreamNeverEndsWithAnEmptyFrame(t *testing.T) {
	s := loaded(t, Clip{File: oggFile(t, t.TempDir(), "a")})
	for i, f := range frames(t, s, 10) {
		if f != "S" {
			t.Fatalf("frame %d = %q, want silence", i, f)
		}
	}
	if i := <-s.ended; i != 0 {
		t.Fatalf("an empty clip ended as %d, want 0", i)
	}
}

func TestLoadFailsOnAMissingLaterClip(t *testing.T) {
	dir := t.TempDir()
	_, err := load([]Clip{{File: oggFile(t, dir, "a", []byte("a1"))}, {File: filepath.Join(dir, "gone.ogg")}})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("load = %v, want fs.ErrNotExist", err)
	}
}
