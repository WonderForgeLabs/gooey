// chromatica: a vendor. A separate process, on the other side of the
// wire, being paid to change an app it did not write.
//
//	go run ./cmd/chromatica -addr 127.0.0.1:7788
//
// This is the whole product. It reads the host's tree to find the seam
// it was sold access to, then replaces one named element with markup of
// its own. It has no library from northwind, no plugin API, no SDK — it
// speaks MCP over HTTP, which is the same door an agent uses and the
// same door a person's keyboard reaches through.
//
// What it deliberately does NOT do, because it cannot:
//
//   - invent a value. Every binding in toolbar.gooey resolves against
//     the HOST's context. Reaching past that map is not a policy
//     violation, it is a load error.
//   - ship an image. <Image Src> takes a literal path in the host's own
//     filesystem or a handle the host already holds. A vendor cannot put
//     one pixel on your screen that you did not ship.
//   - block you. There is no <Frozen> in the markup vocabulary, so an
//     injected sheet can look modal and cannot be modal. You can always
//     tab past it.
//
// And the thing it does not have to do: ask the person sitting in front
// of the screen. The app owner consented once, at process start, by
// calling mcp.Serve. The user has no seam in this system at all — which
// is the point of the demo, and the reason this program is deliberately
// well-behaved. A hostile vendor and a polite one are indistinguishable
// from where the user is sitting.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7788", "the host app's control plane")
	target := flag.String("target", "Toolbar", "the named element to replace")
	src := flag.String("markup", "cmd/chromatica/toolbar.gooey", "this vendor's markup")
	flag.Parse()

	c := &client{url: "http://" + *addr + "/mcp"}

	say("dialling %s", c.url)

	// 1. Look before touching. A vendor that patches an element it has
	//    not read is a vendor that will one day delete your button.
	// No depth limit: a named element can be anywhere, and a truncated
	// snapshot reports "not found" for something that is right there.
	names, err := c.call("tree_snapshot", nil)
	if err != nil {
		die("the host is not listening: %v", err)
	}
	if !bytes.Contains([]byte(names), []byte(`"`+*target+`"`)) {
		die("no element named %q in the host tree — nothing to attach to", *target)
	}
	say("found %s in the host tree", *target)

	// 2. Confirm the binding actually exists before writing markup that
	//    depends on it. The host's Values map is the whole grant.
	vals, err := c.call("list_values", nil)
	if err != nil {
		die("list_values: %v", err)
	}
	if !bytes.Contains([]byte(vals), []byte(`Tint`)) {
		die("the host exposes no Tint handle — this product has nothing to sell here")
	}
	say("host exposes Tint; that is the seam we were paid for")

	// 3. Replace the element. The markup lives in a file, because markup
	//    lives in markup files.
	markup, err := os.ReadFile(*src)
	if err != nil {
		die("reading %s: %v", *src, err)
	}
	out, err := c.call("patch_markup", map[string]any{
		"name":   *target,
		"source": string(markup),
	})
	if err != nil {
		die("patch_markup: %v", err)
	}
	say("patched: %s", compact(out))
	say("")
	say("a colour picker is now in northwind's toolbar.")
	say("northwind was not asked. neither was elan.")
}

type client struct {
	url string
	id  int
}

// call is one MCP tool call. The transport is stateless by design, so
// there is no handshake to hold and no session to resume — a vendor is
// a sequence of one-shot requests, which is also why it can be killed
// and restarted mid-demo without the host noticing.
func (c *client) call(tool string, args map[string]any) (string, error) {
	c.id++
	if args == nil {
		args = map[string]any{}
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      c.id,
		"method":  "tools/call",
		"params":  map[string]any{"name": tool, "arguments": args},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest("POST", c.url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json, text/event-stream")

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var env struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("%s: %s", resp.Status, raw)
	}
	if env.Error != nil {
		return "", fmt.Errorf("%s", env.Error.Message)
	}
	if len(env.Result.Content) == 0 {
		return "", nil
	}
	text := env.Result.Content[0].Text
	if env.Result.IsError {
		return "", fmt.Errorf("%s", text)
	}
	return text, nil
}

func compact(s string) string {
	var v any
	if json.Unmarshal([]byte(s), &v) != nil {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func say(format string, a ...any) {
	fmt.Fprintf(os.Stdout, "chromatica │ "+format+"\n", a...)
}

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "chromatica │ "+format+"\n", a...)
	os.Exit(1)
}
