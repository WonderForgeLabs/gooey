package main

// Audio playback for the deck: one file per beat, named by beat id, in
// audio/. The deck does not synthesize anything — it plays what
// ElevenLabs produced, so the recording's audio and the recording's
// pacing come from the same place.
//
// Confinement: Play and Stop are called from commands and from the Timer
// tick, which is to say from the UI goroutine, so nothing here needs a
// lock. The only other goroutine is the reaper, and it touches no
// property and no field a caller can see — it exists solely so a
// finished player is not left as a zombie.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Player runs an external audio program. There is deliberately no fallback
// to a beep or to silence-with-a-shrug: if no player is on the box, the
// deck says so on screen rather than pretending a beat had audio.
type Player struct {
	bin string
	dir string
	cur *exec.Cmd
}

// candidates are tried in order. ffplay is first because the deck's
// audio is mp3 and ffplay is the only one of these that reads mp3
// without an argument fight; paplay and aplay are wav-only and are here
// for the case where the track has been converted.
var candidates = []struct {
	bin  string
	args []string
}{
	{"ffplay", []string{"-nodisp", "-autoexit", "-loglevel", "quiet"}},
	{"mpv", []string{"--no-video", "--really-quiet"}},
	{"paplay", nil},
	{"aplay", []string{"-q"}},
}

func NewPlayer(dir string) *Player {
	p := &Player{dir: dir}
	for _, c := range candidates {
		if _, err := exec.LookPath(c.bin); err == nil {
			p.bin = c.bin
			return p
		}
	}
	return p
}

func (p *Player) Available() bool { return p.bin != "" }

func (p *Player) args() []string {
	for _, c := range candidates {
		if c.bin == p.bin {
			return c.args
		}
	}
	return nil
}

// Path is where a beat's audio would live. Exported shape rather than a
// private join so the status line can name the missing file exactly —
// "no audio: audio/3.6.mp3" is actionable, "no audio" is not.
func (p *Player) Path(beatID string) string {
	return filepath.Join(p.dir, beatID+".mp3")
}

// Play stops whatever was playing and starts this beat. The returned
// string is a status for the screen, not an error to handle: a missing
// take during a rehearsal is normal.
func (p *Player) Play(beatID string) string {
	p.Stop()
	if !p.Available() {
		return "no audio player found (install ffmpeg)"
	}
	path := p.Path(beatID)
	if _, err := os.Stat(path); err != nil {
		return "no audio: " + path
	}
	cmd := exec.Command(p.bin, append(p.args(), path)...)
	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("%s failed: %v", p.bin, err)
	}
	p.cur = cmd
	// Reap. Wait is the only call, and its result is deliberately
	// dropped: a killed player exits non-zero and that is not news.
	go func() { _ = cmd.Wait() }()
	return "playing " + filepath.Base(path)
}

// Stop kills the current player if there is one. Safe to call when
// nothing is playing, which is why every Play begins with it.
func (p *Player) Stop() {
	if p.cur == nil || p.cur.Process == nil {
		return
	}
	_ = p.cur.Process.Kill()
	p.cur = nil
}

// Missing lists the beats with no audio file, which is the only
// pre-flight check that matters before a recording session.
func (p *Player) Missing(beats []Beat) []string {
	var out []string
	for _, b := range beats {
		if _, err := os.Stat(p.Path(b.ID)); err != nil {
			out = append(out, b.ID)
		}
	}
	return out
}

func summarizeMissing(missing []string, total int) string {
	switch {
	case len(missing) == 0:
		return "audio: all present"
	case len(missing) == total:
		return "audio: none recorded yet"
	default:
		return fmt.Sprintf("audio: %d/%d missing (%s)",
			len(missing), total, strings.Join(missing, " "))
	}
}
