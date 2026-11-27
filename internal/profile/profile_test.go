package profile

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"

	"github.com/be-sandaa/coucou/internal/sounds"
	"github.com/be-sandaa/coucou/internal/store"
)

// write puts body in a temp dir as profile.toml and returns the dir.
func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "profile.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(path)
}

// Only id is required. Everything else falls back to what a bot without a character did before
// profiles existed: no color of its own, so the bot's, a 5% chance, and the other three settings off.
func TestMinimalProfile(t *testing.T) {
	dir := write(t, `id = "gus"`)
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Profile{
		Dir: dir, ID: "gus", Defaults: store.Defaults{Chance: defaultChance},
		Status: Status{Activity: discord.ActivityTypeCustom, Online: discord.OnlineStatusOnline},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if got.SoundsDir() != filepath.Join(dir, "sounds") {
		t.Errorf("SoundsDir = %q, want sounds/ beside profile.toml", got.SoundsDir())
	}
}

const (
	knock = "knock"
	who   = "who"
)

func TestEveryKeyIsRead(t *testing.T) {
	dir := write(t, `
id       = "lenore"
nickname = " Lenore "
application_id = "912694340814516254"
preview  = ["hiss", "creak"]
keywords = [" Goth ", "", "poetry"]
emoji    = "🖤"
color    = "#4E5058"
tagline  = "Mean, nicely."
lore     = """
Came anyway.
"""
traits   = ["sits in silence", "leaves without a sound"]

[defaults]
chance   = 0
suspense = 20
fakeout  = 50
encore   = 50

[chaining]
chance   = 80
decay    = 0.7
max_hops = 5

[[chains]]
sound = "knock"
next  = [{ sound = "who", weight = 70, after = 2 }, { sound = "rim", weight = 30 }]

[[chains]]
sound = "who"
next  = [{ sound = "knock", weight = 1, after = 0.35 }]

[[links]]
from = "snare"
to   = { boo = 70, sad-trombone = 30 }

[status]
text     = " 🖤 lurking "
activity = "listening"
online   = "dnd"
`)
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Profile{
		Dir: dir, ID: "lenore", Nickname: "Lenore", Emoji: "🖤", Color: 0x4E5058,
		Tagline: "Mean, nicely.", Lore: "Came anyway.", Traits: []string{"sits in silence", "leaves without a sound"},
		// An explicit 0 chance is opt-in, not "unset": it must not fall back to 5.
		Defaults: store.Defaults{Chance: 0, Suspense: MaxSuspense, FakeOut: MaxFakeOut, Encore: MaxEncore},
		Chains: sounds.Chains{Chance: 80, Decay: 0.7, MaxHops: 5, Next: map[string][]sounds.Edge{
			knock: {{Sound: who, Weight: 70, After: 2 * time.Second}, {Sound: "rim", Weight: 30}},
			who:   {{Sound: knock, Weight: 1, After: 350 * time.Millisecond}},
		}},
		Links:    sounds.Links{"snare": {"boo": 70, "sad-trombone": 30}},
		Status:   Status{Text: "🖤 lurking", Activity: discord.ActivityTypeListening, Online: discord.OnlineStatusDND},
		App:      912694340814516254,
		Preview:  []string{"hiss", "creak"},
		Keywords: []string{"goth", "poetry"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

// Chains without a [chaining] table play at 100%, no decay, 8 hops; no chains leaves the graph empty.
func TestChainingDefaults(t *testing.T) {
	got, err := Load(write(t, "id = \"x\"\n[[chains]]\nsound = \"a\"\nnext = [{ sound = \"a\", weight = 1 }]"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := sounds.Chains{Chance: 100, Decay: 1, MaxHops: 8, Next: map[string][]sounds.Edge{"a": {{Sound: "a", Weight: 1}}}}
	if !reflect.DeepEqual(got.Chains, want) {
		t.Errorf("Chains = %+v, want %+v", got.Chains, want)
	}
	got, err = Load(write(t, `id = "x"`))
	if err != nil || !reflect.DeepEqual(got.Chains, sounds.Chains{}) {
		t.Errorf("no chains: Chains = %+v, %v; want zero", got.Chains, err)
	}
}

// The example people copy has to load as it stands.
func TestExampleProfileLoads(t *testing.T) {
	if _, err := load("../../profile.example.toml", t.TempDir()); err != nil {
		t.Errorf("Load: %v", err)
	}
}

func TestInvalidProfiles(t *testing.T) {
	tests := []struct{ name, body string }{
		{"no id", `nickname = "x"`},
		{"id with capitals", `id = "Lenore"`},
		{"id with a space", `id = "le nore"`},
		{"unknown key", "id = \"x\"\nname = \"x\""},
		{"unknown default", "id = \"x\"\n[defaults]\nvolume = 3"},
		{"colour without #", "id = \"x\"\ncolor = \"4E5058\""},
		{"colour too short", "id = \"x\"\ncolor = \"#FFF\""},
		{"colour not hex", "id = \"x\"\ncolor = \"#GGGGGG\""},
		{"chance above 100", "id = \"x\"\n[defaults]\nchance = 101"},
		{"chance negative", "id = \"x\"\n[defaults]\nchance = -1"},
		{"suspense above 20", "id = \"x\"\n[defaults]\nsuspense = 21"},
		{"fakeout above 50", "id = \"x\"\n[defaults]\nfakeout = 51"},
		{"encore above 50", "id = \"x\"\n[defaults]\nencore = 51"},
		{"chaining chance above 100", "id = \"x\"\n[chaining]\nchance = 101"},
		{"chaining chance negative", "id = \"x\"\n[chaining]\nchance = -1"},
		{"decay above 1", "id = \"x\"\n[chaining]\ndecay = 1.1"},
		{"decay negative", "id = \"x\"\n[chaining]\ndecay = -0.1"},
		{"decay nan", "id = \"x\"\n[chaining]\ndecay = nan"},
		{"max hops 0", "id = \"x\"\n[chaining]\nmax_hops = 0"},
		{"max hops above 20", "id = \"x\"\n[chaining]\nmax_hops = 21"},
		{"unknown chaining key", "id = \"x\"\n[chaining]\nhops = 3"},
		{"chain without a sound", "id = \"x\"\n[[chains]]\nnext = [{ sound = \"b\", weight = 1 }]"},
		{"chain with no next", "id = \"x\"\n[[chains]]\nsound = \"a\""},
		{"edge without a sound", "id = \"x\"\n[[chains]]\nsound = \"a\"\nnext = [{ weight = 1 }]"},
		{"edge weight 0", "id = \"x\"\n[[chains]]\nsound = \"a\"\nnext = [{ sound = \"b\" }]"},
		{"edge gap above 20", "id = \"x\"\n[[chains]]\nsound = \"a\"\nnext = [{ sound = \"b\", weight = 1, after = 21 }]"},
		{"edge gap just above 20", "id = \"x\"\n[[chains]]\nsound = \"a\"\nnext = [{ sound = \"b\", weight = 1, after = 20.001 }]"},
		{"edge gap negative", "id = \"x\"\n[[chains]]\nsound = \"a\"\nnext = [{ sound = \"b\", weight = 1, after = -0.2 }]"},
		{"edge gap nan", "id = \"x\"\n[[chains]]\nsound = \"a\"\nnext = [{ sound = \"b\", weight = 1, after = nan }]"},
		{"unknown edge key", "id = \"x\"\n[[chains]]\nsound = \"a\"\nnext = [{ sound = \"b\", weight = 1, delay = 1 }]"},
		{"old steps format", "id = \"x\"\n[[chains]]\nsteps = [{ sound = \"a\" }, { sound = \"b\" }]"},
		{"two chains, one sound", "id = \"x\"\n[[chains]]\nsound = \"a\"\nnext = [{ sound = \"b\", weight = 1 }]\n[[chains]]\nsound = \"a\"\nnext = [{ sound = \"c\", weight = 1 }]"},
		{"link without from", "id = \"x\"\n[[links]]\nto = { b = 1 }"},
		{"link to nothing", "id = \"x\"\n[[links]]\nfrom = \"a\""},
		{"link weight 0", "id = \"x\"\n[[links]]\nfrom = \"a\"\nto = { b = 0 }"},
		{"linked from twice", "id = \"x\"\n[[links]]\nfrom = \"a\"\nto = { b = 1 }\n[[links]]\nfrom = \"a\"\nto = { c = 1 }"},
		{"unknown activity", "id = \"x\"\n[status]\nactivity = \"streaming\""},
		{"unknown online state", "id = \"x\"\n[status]\nonline = \"invisible\""},
		{"status text over 128 characters", "id = \"x\"\n[status]\ntext = \"" + strings.Repeat("🖤", MaxStatus+1) + "\""},
		{"four preview sounds", "id = \"x\"\npreview = [\"a\", \"b\", \"c\", \"d\"]"},
		{"application id not a number", "id = \"x\"\napplication_id = \"lenore\""},
		{"not toml", "id ="},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load(write(t, tc.body)); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestMissingProfileIsAnError(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("expected an error for a directory with no profile.toml")
	}
}

// character puts a profile.toml with body under root/sub.
func character(t *testing.T, root, sub, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, sub), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, sub, "profile.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Every subdirectory with a profile.toml is a character, sorted by id rather than by folder; a
// folder without one is skipped, so a stray directory is not an error.
func TestLoadAll(t *testing.T) {
	root := t.TempDir()
	character(t, root, "a", `id = "zelda"`)
	character(t, root, "b", `id = "bart"`)
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o750); err != nil {
		t.Fatal(err)
	}
	got, err := LoadAll(root)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(got) != 2 || got[0].ID != "bart" || got[1].ID != "zelda" {
		t.Fatalf("got %+v, want bart then zelda", got)
	}
	if got[0].Dir != filepath.Join(root, "b") {
		t.Errorf("Dir = %q, want its own folder", got[0].Dir)
	}
}

func TestLoadAllRefuses(t *testing.T) {
	t.Run("no characters", func(t *testing.T) {
		if _, err := LoadAll(t.TempDir()); err == nil {
			t.Error("expected an error")
		}
	})
	t.Run("one id twice", func(t *testing.T) {
		root := t.TempDir()
		character(t, root, "a", `id = "bart"`)
		character(t, root, "b", `id = "bart"`)
		if _, err := LoadAll(root); err == nil {
			t.Error("expected an error")
		}
	})
	t.Run("one broken character", func(t *testing.T) {
		root := t.TempDir()
		character(t, root, "a", `id = "bart"`)
		character(t, root, "b", `id = "Bad"`)
		if _, err := LoadAll(root); err == nil {
			t.Error("expected an error")
		}
	})
}

// A PNG's first bytes are all http.DetectContentType needs to call it one.
const avatarPNG = "avatar.png"

var png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func TestAvatar(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string][]byte
		want    bool
		wantErr bool
	}{
		{"none", nil, false, false},
		{"a png", map[string][]byte{avatarPNG: png}, true, false},
		{"two of them", map[string][]byte{avatarPNG: png, "avatar.gif": png}, false, true},
		{"not an image", map[string][]byte{avatarPNG: []byte("not an image")}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := write(t, `id = "gus"`)
			for name, data := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			p, err := Load(dir)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Load: %v, want an error: %v", err, tt.wantErr)
			}
			if got := p.Avatar != nil && len(p.AvatarHash) == 16; got != tt.want {
				t.Errorf("avatar %+v, hash %q; want one: %v", p.Avatar, p.AvatarHash, tt.want)
			}
		})
	}
}
