// The test fixture: a tiny portable process the exec pack's tests run
// instead of guessing which OS binaries exist. Built by TestMain into a
// temp dir; never committed as a binary.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "helper: need a subcommand")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "out": // print remaining args to stdout, one line
		fmt.Println(strings.Join(os.Args[2:], " "))
	case "raw": // like out, but without the trailing newline
		fmt.Print(strings.Join(os.Args[2:], " "))
	case "err": // print remaining args to stderr, one line
		fmt.Fprintln(os.Stderr, strings.Join(os.Args[2:], " "))
	case "mix": // one line to each stream, stdout first
		fmt.Println("OUT")
		fmt.Fprintln(os.Stderr, "ERR")
	case "fail": // "boom" on stderr, then exit with the given code
		fmt.Fprintln(os.Stderr, "boom")
		code, _ := strconv.Atoi(os.Args[2])
		os.Exit(code)
	case "exit": // exit silently with the given code
		code, _ := strconv.Atoi(os.Args[2])
		os.Exit(code)
	case "env": // print the environment, sorted, one K=V per line
		env := os.Environ()
		sort.Strings(env)
		for _, kv := range env {
			fmt.Println(kv)
		}
	case "json": // a fixed JSON document for jq tests
		fmt.Println(`{"items":[{"name":"alpha","n":1},{"name":"beta","n":2}],"ok":true}`)
	case "args": // print each remaining arg on its own line
		for _, a := range os.Args[2:] {
			fmt.Println(a)
		}
	case "sleep": // sleep the given number of milliseconds
		ms, _ := strconv.Atoi(os.Args[2])
		time.Sleep(time.Duration(ms) * time.Millisecond)
	case "stubborn": // ignore SIGTERM, then sleep — the escalation test
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(30 * time.Second)
	default:
		fmt.Fprintf(os.Stderr, "helper: unknown subcommand %q\n", os.Args[1])
		os.Exit(2)
	}
}
