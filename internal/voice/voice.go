package voice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
)

const (
	joinTimeout = 10 * time.Second
	// Healthy handshakes settled in under 1.5s in production; a stalled one never settles, so a
	// longer wait only delays the rejoin that does get through.
	daveTimeout = 3 * time.Second
	// ClipTimeout bounds one clip; a visit playing several gets one each.
	ClipTimeout = 30 * time.Second
	// joinChime is how long Discord's join sound covers the start of a clip played straight after
	// joining. Suspense counts towards it, so only a shorter one waits the difference.
	joinChime = time.Second
)

// The stage a play reached. Returned to the caller, which stores it as "<stage>_fail".
const (
	StageJoin     = "join"
	StageSuspense = "suspense"
	StagePlay     = "play"
)

var (
	ErrBusy         = errors.New("already connected in this guild")
	ErrEveryoneLeft = errors.New("everyone left")
	// ErrPlayTimeout is a clip that outran ClipTimeout. A sentinel rather than an inline error so
	// the caller can tell a truncation apart from a stage that actually broke: the audio played, it
	// just did not finish, and the fix is to re-encode the file rather than to look at the bot.
	ErrPlayTimeout = errors.New("play timeout")
	errDaveStalled = errors.New("dave handshake timeout")
)

// Clip is one file a visit plays and the silence before it, which the first clip's suspense covers.
type Clip struct {
	File string
	Gap  time.Duration
}

type Opts struct {
	Suspense time.Duration // silence between joining and playing
	// StillPopulated is polled after suspense and before every later clip; return false to stop
	// instead of playing to an empty room.
	StillPopulated func() bool
	// Silent leaves after suspense without playing: the fake-out.
	Silent bool
	// Leaving runs on every return once a join was attempted, while the bot is still in the channel.
	Leaving func()
}

// busy tracks guilds we're currently in (value: cancel func), so two ticks / a command can't
// double-join, and /leave can cut a play short.
var busy sync.Map // guildID → context.CancelFunc

func Busy(guild snowflake.ID) bool { _, ok := busy.Load(guild); return ok }

// Cancel aborts an in-progress play in the guild, if any. The deferred Close still runs.
func Cancel(guild snowflake.ID) bool {
	if v, ok := busy.Load(guild); ok {
		if cancel, ok := v.(context.CancelFunc); ok {
			cancel()
			return true
		}
	}
	return false
}

// Shutdown cuts every play short, as /leave does, and waits for each to leave. busy.Delete runs
// after a play's own conn.Close, so an empty map means no conn is still mid-teardown when the
// client's VoiceManager.Close would force it.
func Shutdown(ctx context.Context) {
	busy.Range(func(_, v any) bool {
		if cancel, ok := v.(context.CancelFunc); ok {
			cancel()
		}
		return true
	})
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	for {
		empty := true
		busy.Range(func(_, _ any) bool { empty = false; return false })
		if empty {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Play joins, optionally sits in silence, plays each clip in turn on the one connection, leaves.
// Always leaves. Returns the stage it failed in ("join", "suspense", "play") for the stats table, and
// how many clips finished.
func Play(ctx context.Context, client *bot.Client, guild, channel snowflake.ID, clips []Clip, o Opts) (stage string, played int, err error) { //nolint:gocyclo // one error branch per join/dave/suspense/play stage; splitting hides the sequence
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if _, loaded := busy.LoadOrStore(guild, cancel); loaded {
		return StageJoin, 0, ErrBusy
	}
	defer busy.Delete(guild)

	// Loaded before joining, so a missing or broken clip fails the visit before anyone sees the bot.
	var st *stream
	if !o.Silent {
		if st, err = load(clips); err != nil {
			return StagePlay, 0, err
		}
	}

	stage = StageJoin
	conn, err := join(ctx, client, guild, channel)
	defer leave(conn)
	if o.Leaving != nil {
		defer o.Leaving()
	}
	if err != nil {
		return stage, 0, err
	}

	if o.Suspense > 0 {
		stage = StageSuspense
		if err := sleep(ctx, o.Suspense); err != nil {
			return stage, 0, err
		}
		if o.StillPopulated != nil && !o.StillPopulated() {
			return stage, 0, ErrEveryoneLeft
		}
	}
	if o.Silent {
		return stage, 0, nil
	}

	stage = StagePlay
	if err = sleep(ctx, joinChime-o.Suspense); err != nil {
		return stage, 0, err
	}
	if err = conn.SetSpeaking(ctx, voice.SpeakingFlagMicrophone); err != nil {
		return stage, 0, err
	}
	if played, err = playClips(ctx, conn, st, clips, o.StillPopulated); err != nil {
		return stage, played, err
	}
	// let the last frame drain before tearing the UDP socket down
	time.Sleep(100 * time.Millisecond)
	return stage, played, nil
}

// playClips hands conn the whole visit as one stream and returns how many clips finished. Each clip
// gets its gap and ClipTimeout to end in; after one ends, an emptied room stops the visit early
// rather than failing it.
func playClips(ctx context.Context, conn voice.Conn, st *stream, clips []Clip, stillPopulated func() bool) (played int, err error) {
	conn.SetOpusFrameProvider(st)
	for i, c := range clips {
		t := time.NewTimer(c.Gap + ClipTimeout)
		select {
		case <-st.ended:
			t.Stop()
			played++
		case <-t.C:
			return played, ErrPlayTimeout
		case <-ctx.Done():
			t.Stop()
			return played, ctx.Err()
		}
		if i < len(clips)-1 && stillPopulated != nil && !stillPopulated() {
			return played, nil
		}
	}
	return played, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// join connects and waits out the DAVE handshake, rejoining once if it stalls: Discord sometimes
// never answers the bot's key package, and a fresh connection gets through where waiting does not.
// The returned conn is never nil and is the caller's to leave, error or not.
func join(ctx context.Context, client *bot.Client, guild, channel snowflake.ID) (voice.Conn, error) {
	for attempt := 1; ; attempt++ {
		conn := client.VoiceManager.CreateConn(guild)
		err := open(ctx, conn, channel)
		if attempt == 2 || !errors.Is(err, errDaveStalled) || ctx.Err() != nil {
			return conn, err
		}
		slog.Warn("rejoining after a stalled dave handshake", slog.Any("guild", guild), slog.Any("err", err))
		leave(conn)
	}
}

func open(ctx context.Context, conn voice.Conn, channel snowflake.ID) error {
	jctx, cancel := context.WithTimeout(ctx, joinTimeout)
	defer cancel()
	if err := conn.Open(jctx, channel, false, true); err != nil { // selfMute=false, selfDeaf=true
		return err
	}
	// DAVE (E2EE) has to finish its MLS handshake before frames are accepted.
	dctx, dcancel := context.WithTimeout(ctx, daveTimeout)
	defer dcancel()
	return waitDave(dctx, conn)
}

func leave(conn voice.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn.Close(ctx) // also drops the conn from the manager
	// After conn.Close, not before: closing the session first could cut off an MLS message the
	// conn is still sending as it tears down.
	closeSession(conn)
}

func waitDave(ctx context.Context, conn any) error {
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	for !daveReady(conn) {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w (%s)", errDaveStalled, daveState(conn))
		case <-t.C:
		}
	}
	return nil
}

// stream is a visit's clips demuxed up front and played as one provider: each clip's packets, then
// the next clip's gap as silence frames, then its packets. Past the last clip it plays silence
// forever: an empty frame makes disgo v0.20.0 call SetSpeaking(None), which deadlocks against
// conn.Close and wedges gateway dispatch. Only disgo's sender goroutine calls ProvideOpusFrame.
type stream struct {
	clips [][][]byte
	gaps  []int    // silence frames before each clip; the first clip's gap is suspense's, so 0
	ended chan int // each clip's index as its last packet goes out; holds them all, never blocks

	clip, packet, silence int
}

// load reads and demuxes every clip, failing on the first that cannot be read.
func load(clips []Clip) (*stream, error) {
	st := &stream{clips: make([][][]byte, len(clips)), gaps: make([]int, len(clips)), ended: make(chan int, len(clips))}
	for i, c := range clips {
		b, err := os.ReadFile(c.File)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", c.File, err)
		}
		r := newOggOpusReader(bytes.NewReader(b))
		for {
			p, err := r.ProvideOpusFrame()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("load %s: %w", c.File, err)
			}
			st.clips[i] = append(st.clips[i], p)
		}
		if i > 0 {
			st.gaps[i] = int((c.Gap + frame - 1) / frame) // rounded up: a gap is never shorter than asked
		}
	}
	return st, nil
}

// frame is how long disgo's sender spends on each frame it is handed.
const frame = voice.OpusFrameSizeMs * time.Millisecond

func (s *stream) ProvideOpusFrame() ([]byte, error) {
	for s.clip < len(s.clips) {
		if s.silence > 0 {
			s.silence--
			return voice.SilenceAudioFrame, nil
		}
		packets := s.clips[s.clip]
		if s.packet < len(packets) {
			p := packets[s.packet]
			if s.packet++; s.packet == len(packets) {
				s.next()
			}
			return p, nil
		}
		s.next() // a clip with no packets still ends
	}
	return voice.SilenceAudioFrame, nil
}

func (s *stream) next() {
	s.ended <- s.clip
	s.clip, s.packet = s.clip+1, 0
	if s.clip < len(s.clips) {
		s.silence = s.gaps[s.clip]
	}
}

func (s *stream) Close() {}
