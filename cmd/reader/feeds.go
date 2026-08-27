package main

import (
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey/render"
)

type Story struct {
	Title, Link, Author, Body string
	Published                 string
}

type Feed struct {
	Title   string
	URL     string
	Stories []Story
	Err     error
	Loading bool
}

// ---- fetch + sniff ----

func fetchFeed(url string) *Feed {
	f := &Feed{Title: url, URL: url}
	client := &http.Client{Timeout: 12 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		f.Err = err
		return f
	}
	req.Header.Set("User-Agent", "gooey-reader/0.1")
	resp, err := client.Do(req)
	if err != nil {
		f.Err = err
		return f
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		f.Err = fmt.Errorf("HTTP %d", resp.StatusCode)
		return f
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		f.Err = err
		return f
	}
	title, stories, err := parseFeed(data)
	if err != nil {
		f.Err = err
		return f
	}
	f.Title, f.Stories = title, stories
	return f
}

// parseFeed sniffs RSS 2.0 vs Atom by root element.
func parseFeed(data []byte) (string, []Story, error) {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", nil, fmt.Errorf("not a feed: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok {
			switch se.Name.Local {
			case "rss", "RDF":
				return parseRSS(data)
			case "feed":
				return parseAtom(data)
			default:
				return "", nil, fmt.Errorf("unknown feed root <%s>", se.Name.Local)
			}
		}
	}
}

type rssDoc struct {
	Channel struct {
		Title string `xml:"title"`
		Items []struct {
			Title   string `xml:"title"`
			Link    string `xml:"link"`
			Creator string `xml:"creator"`
			PubDate string `xml:"pubDate"`
			Desc    string `xml:"description"`
			Encoded string `xml:"encoded"` // content:encoded
		} `xml:"item"`
	} `xml:"channel"`
}

func parseRSS(data []byte) (string, []Story, error) {
	var d rssDoc
	if err := xml.Unmarshal(data, &d); err != nil {
		return "", nil, err
	}
	var out []Story
	for _, it := range d.Channel.Items {
		body := it.Encoded
		if body == "" {
			body = it.Desc
		}
		out = append(out, Story{
			Title: strings.TrimSpace(it.Title), Link: it.Link,
			Author: it.Creator, Published: shortDate(it.PubDate),
			Body: htmlToText(body),
		})
	}
	return strings.TrimSpace(d.Channel.Title), out, nil
}

type atomDoc struct {
	Title   string `xml:"title"`
	Entries []struct {
		Title string `xml:"title"`
		Links []struct {
			Rel  string `xml:"rel,attr"`
			Href string `xml:"href,attr"`
		} `xml:"link"`
		Author struct {
			Name string `xml:"name"`
		} `xml:"author"`
		Updated string `xml:"updated"`
		Content string `xml:"content"`
		Summary string `xml:"summary"`
	} `xml:"entry"`
}

func parseAtom(data []byte) (string, []Story, error) {
	var d atomDoc
	if err := xml.Unmarshal(data, &d); err != nil {
		return "", nil, err
	}
	var out []Story
	for _, e := range d.Entries {
		link := ""
		for _, l := range e.Links {
			if l.Rel == "" || l.Rel == "alternate" {
				link = l.Href
				break
			}
		}
		body := e.Content
		if body == "" {
			body = e.Summary
		}
		out = append(out, Story{
			Title: strings.TrimSpace(e.Title), Link: link,
			Author: e.Author.Name, Published: shortDate(e.Updated),
			Body: htmlToText(body),
		})
	}
	return strings.TrimSpace(d.Title), out, nil
}

func shortDate(s string) string {
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("Jan 02 15:04")
		}
	}
	if len(s) > 16 {
		return s[:16]
	}
	return s
}

// ---- HTML → text ----

var (
	reTag    = regexp.MustCompile(`(?s)<(script|style)[^>]*>.*?</(script|style)>`)
	reBreak  = regexp.MustCompile(`(?i)<(/p|br[^>]*|/div|/li|/h[1-6]|/blockquote)>`)
	reAnyTag = regexp.MustCompile(`(?s)<[^>]*>`)
	reBlank  = regexp.MustCompile(`\n{3,}`)
)

func htmlToText(s string) string {
	s = reTag.ReplaceAllString(s, "")
	s = reBreak.ReplaceAllString(s, "\n\n")
	s = reAnyTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = reBlank.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// wrap breaks text into lines of at most w display COLUMNS, preserving
// paragraphs.
func wrap(s string, w int) []string {
	if w < 4 {
		w = 4
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := ""
		for _, word := range words {
			switch {
			case line == "":
				line = word
			// COLUMNS, which is the third answer this line has had and
			// the first correct one.
			//
			// len() came first and made every non-ASCII paragraph wrap
			// early — a smart quote costs three bytes and one cell.
			// Rune counts fixed that and were still wrong the other way:
			// a CJK or emoji rune is one rune and TWO cells, so a line
			// of them overran its column by up to its own length. This
			// is a feed reader, so the text is arbitrary prose off the
			// internet — the one place in this repo guaranteed to meet
			// both cases.
			//
			// The note that used to sit here said "nothing in this repo
			// is width-aware". That stopped being true in #358;
			// render.StringWidth counts by grapheme cluster, so a flag
			// emoji measures two columns rather than two runes.
			case render.StringWidth(line)+1+render.StringWidth(word) <= w:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
		}
		out = append(out, line)
	}
	return out
}

// ---- OPML ----

type opmlDoc struct {
	XMLName xml.Name      `xml:"opml"`
	Version string        `xml:"version,attr"`
	Title   string        `xml:"head>title"`
	Body    []opmlOutline `xml:"body>outline"`
}

type opmlOutline struct {
	Text     string        `xml:"text,attr"`
	XMLURL   string        `xml:"xmlUrl,attr,omitempty"`
	Children []opmlOutline `xml:"outline"`
}

// parseOPML flattens nested outlines to feed URLs.
func parseOPML(data []byte) ([]string, error) {
	var d opmlDoc
	if err := xml.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	var urls []string
	var walk func([]opmlOutline)
	walk = func(os []opmlOutline) {
		for _, o := range os {
			if o.XMLURL != "" {
				urls = append(urls, o.XMLURL)
			}
			walk(o.Children)
		}
	}
	walk(d.Body)
	return urls, nil
}

func writeOPML(path string, feeds []*Feed) error {
	d := opmlDoc{Version: "2.0", Title: "gooey reader feeds"}
	for _, f := range feeds {
		d.Body = append(d.Body, opmlOutline{Text: f.Title, XMLURL: f.URL})
	}
	data, err := xml.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(xml.Header), data...), 0o644)
}
