#!/bin/sh
# watchgo.sh — rebuild and restart a Go program when its source is saved,
# with the compile shown on screen.
#
#   watchgo.sh <dir> <source-file> <binary> [pause]
#
# This is the Go half of the deck's two live-edit claims, and the two are
# only interesting next to each other. Beat 3.5 edits MARKUP and nothing
# restarts: the running tree picks the file up and the state survives.
# Here the source is Go, so there is a compiler between the save and the
# screen and the process has to die. Showing the build is what makes that
# difference visible rather than asserted — so the output is the point,
# not noise to hide behind a spinner.
#
# Confinement: this runs as a child process on a <Terminal>'s pty. Its
# output is modelled by render.Screen and blitted into the slide; it
# touches no property and knows nothing about the deck.
set -u

dir=${1:?usage: watchgo.sh <dir> <file> <binary> [pause]}
src=${2:?usage: watchgo.sh <dir> <file> <binary> [pause]}
bin=${3:?usage: watchgo.sh <dir> <file> <binary> [pause]}
pause=${4:-1.2}

cd "$dir" || exit 1

# The running child, empty while nothing is running. At this scope because
# the save-watcher below has to be able to stop it.
pid=

stamp() { stat -c %Y "$src" 2>/dev/null || echo 0; }

stop() {
	[ -n "$pid" ] || return 0
	# SIGTERM, not SIGKILL: gooey's Run treats SIGINT/SIGTERM as an end to
	# the run and hands the terminal back on the way out (app.go:381). Kill
	# it outright and the pty is left in raw mode, so every line this script
	# prints afterwards staircases down the pane.
	kill "$pid" 2>/dev/null
	i=0
	while kill -0 "$pid" 2>/dev/null && [ "$i" -lt 20 ]; do
		sleep 0.1
		i=$((i + 1))
	done
	if kill -0 "$pid" 2>/dev/null; then
		kill -9 "$pid" 2>/dev/null
		# Only reachable when the app ignored SIGTERM for two seconds, which
		# means it did NOT restore the terminal. Undo raw mode by hand rather
		# than spend the rest of the talk printing staircases.
		stty sane 2>/dev/null
	fi
	wait "$pid" 2>/dev/null
	pid=
}

trap 'stop; exit 0' INT TERM HUP

while :; do
	was=$(stamp)

	printf '\033[2J\033[H'
	printf '$ go build -o %s .\n' "$bin"

	if out=$(go build -o "$bin" . 2>&1); then
		[ -n "$out" ] && printf '%s\n' "$out"
		printf '  ok\n'
		# A deliberate beat. Without it the compile is one frame nobody in
		# the room can read, and "show the compilation" is not served by a
		# flicker. Pass 0 to turn it off.
		sleep "$pause"
		printf '\033[2J\033[H'
		./"$bin" &
		pid=$!
	else
		printf '%s\n' "$out"
		printf '\n-- build failed. fix it and save again. --\n'
		pid=
	fi

	# Wait for a save. Polling mtime rather than inotifywait, which is not
	# installed here and whose absence would be a dead pane on stage. 300ms
	# is well inside the gap between :w and looking up.
	while [ "$(stamp)" = "$was" ]; do
		# The presenter may quit the app itself. Say so instead of leaving a
		# blank pane that looks like a crash, and keep watching.
		if [ -n "$pid" ] && ! kill -0 "$pid" 2>/dev/null; then
			wait "$pid" 2>/dev/null
			pid=
			printf '\n-- program exited. save %s to build and run it again. --\n' "$src"
		fi
		sleep 0.3
	done

	stop
done
