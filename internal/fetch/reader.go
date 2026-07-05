// Package fetch turns a public URL into readable Markdown for LLM
// consumption (the url_read tool and the answer pipeline). It is the only
// part of Searchgirl that fetches arbitrary URLs, so the SSRF guard lives
// here: private, loopback and link-local addresses are refused at dial time
// (which also covers DNS rebinding and redirects).
package fetch

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/html"
)

type Reader struct {
	MaxBytes     int64 // download cap (default 2 MiB)
	AllowPrivate bool  // tests and explicitly-opted intranets only
	Client       *http.Client
}

// FromEnv honors SEARCHGIRL_FETCH_MAX_BYTES and SEARCHGIRL_FETCH_ALLOW_PRIVATE.
func FromEnv() *Reader {
	r := &Reader{MaxBytes: 2 << 20}
	if v := os.Getenv("SEARCHGIRL_FETCH_MAX_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			r.MaxBytes = n
		}
	}
	r.AllowPrivate = os.Getenv("SEARCHGIRL_FETCH_ALLOW_PRIVATE") == "1"
	return r
}

type Doc struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	Markdown  string `json:"markdown"`
	Truncated bool   `json:"truncated"`
	FetchedAt string `json:"fetched_at"`
}

// Read fetches the URL and returns its main content as Markdown. maxLength
// caps the output in characters (0 = 20000). raw skips content extraction
// and converts the whole body.
func (r *Reader) Read(ctx context.Context, rawURL string, maxLength int, raw bool) (*Doc, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("url_read needs an absolute http(s) URL")
	}
	if maxLength <= 0 {
		maxLength = 20000
	}

	client := r.Client
	if client == nil {
		client = r.newGuardedClient()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Searchgirl/0.1 (+https://github.com/diegoparras/searchgirl)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch: http %d from %s", resp.StatusCode, u.Host)
	}

	maxBytes := r.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 2 << 20
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	truncatedBytes := int64(len(body)) > maxBytes
	if truncatedBytes {
		body = body[:maxBytes]
	}

	doc := &Doc{URL: u.String(), FetchedAt: time.Now().UTC().Format(time.RFC3339)}
	ct := resp.Header.Get("Content-Type")
	switch {
	case strings.Contains(ct, "text/html"), strings.Contains(ct, "application/xhtml"):
		title, md := htmlToMarkdown(string(body), !raw)
		doc.Title, doc.Markdown = title, md
	case strings.HasPrefix(ct, "text/"):
		doc.Markdown = string(body)
	case strings.Contains(ct, "json"):
		doc.Markdown = "```json\n" + string(body) + "\n```"
	default:
		return nil, fmt.Errorf("fetch: unsupported content type %q (only html, text and json)", ct)
	}

	doc.Markdown = strings.TrimSpace(doc.Markdown)
	if runes := []rune(doc.Markdown); len(runes) > maxLength {
		doc.Markdown = strings.TrimSpace(string(runes[:maxLength])) + "\n\n[… truncado]"
		doc.Truncated = true
	}
	doc.Truncated = doc.Truncated || truncatedBytes
	return doc, nil
}

// ServeThumb streams an image from a public URL through the same SSRF guard.
// It exists for the UI's thumbnails: the browser only ever talks to
// Searchgirl, never to the engines' image hosts (privacy + no hotlink walls).
func (r *Reader) ServeThumb(w http.ResponseWriter, req *http.Request) {
	raw := strings.TrimSpace(req.URL.Query().Get("u"))
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		http.Error(w, "bad thumbnail url", http.StatusBadRequest)
		return
	}
	client := r.Client
	if client == nil {
		client = r.newGuardedClient()
	}
	out, err := http.NewRequestWithContext(req.Context(), http.MethodGet, u.String(), nil)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	out.Header.Set("User-Agent", "Searchgirl/0.1 (+https://github.com/diegoparras/searchgirl)")
	out.Header.Set("Accept", "image/*")
	resp, err := client.Do(out)
	if err != nil {
		http.NotFound(w, req)
		return
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(ct, "image/") {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 5<<20))
}

// newGuardedClient builds an http.Client whose dialer refuses non-public
// addresses at connect time. Checking the resolved IP (not the hostname)
// closes the DNS-rebinding hole, and redirects re-enter the same check.
func (r *Reader) newGuardedClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(network, address string, c syscall.RawConn) error {
			if r.AllowPrivate {
				return nil
			}
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || !isPublicIP(ip) {
				return fmt.Errorf("refusing to fetch non-public address %s", host)
			}
			return nil
		},
	}
	return &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			MaxIdleConns:        10,
			TLSHandshakeTimeout: 10 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}

// extraReserved cubre rangos que net.IP no clasifica como privados pero que
// no deben alcanzarse desde un fetch público: Carrier-Grade NAT (RFC 6598) y
// el bloque reservado 240.0.0.0/4 (RFC 5735). Endurece el guard SSRF más allá
// de lo que trae la stdlib.
var extraReserved = func() []*net.IPNet {
	var nets []*net.IPNet
	for _, cidr := range []string{"100.64.0.0/10", "240.0.0.0/4"} {
		if _, n, err := net.ParseCIDR(cidr); err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}()

func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	for _, n := range extraReserved {
		if n.Contains(ip) {
			return false
		}
	}
	return true
}

// --- HTML → Markdown ---

var skipTags = map[string]bool{
	"script": true, "style": true, "noscript": true, "svg": true,
	"iframe": true, "form": true, "button": true, "input": true,
}

// chromeTags are layout/navigation containers dropped in extract mode.
var chromeTags = map[string]bool{
	"nav": true, "header": true, "footer": true, "aside": true,
}

// htmlToMarkdown converts HTML to compact Markdown. With extract=true it
// drops navigation/chrome containers — a pragmatic readability pass that
// keeps the dependency budget at x/net/html.
func htmlToMarkdown(src string, extract bool) (title, md string) {
	root, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return "", strings.TrimSpace(src)
	}
	var b strings.Builder
	var walk func(n *html.Node, pre bool)
	walk = func(n *html.Node, pre bool) {
		switch n.Type {
		case html.ElementNode:
			tag := n.Data
			if skipTags[tag] || (extract && chromeTags[tag]) {
				return
			}
			switch tag {
			case "title":
				if title == "" {
					title = strings.TrimSpace(textOf(n))
				}
				return
			case "h1", "h2", "h3", "h4", "h5", "h6":
				b.WriteString("\n\n" + strings.Repeat("#", int(tag[1]-'0')) + " ")
				walkChildren(n, walk, pre)
				b.WriteString("\n")
				return
			case "p", "div", "section", "article", "main", "tr":
				b.WriteString("\n")
				walkChildren(n, walk, pre)
				b.WriteString("\n")
				return
			case "br":
				b.WriteString("\n")
				return
			case "li":
				b.WriteString("\n- ")
				walkChildren(n, walk, pre)
				return
			case "a":
				href := attr(n, "href")
				text := strings.TrimSpace(textOf(n))
				if text == "" {
					return
				}
				if href != "" && strings.HasPrefix(href, "http") {
					fmt.Fprintf(&b, "[%s](%s)", text, href)
				} else {
					b.WriteString(text)
				}
				return
			case "pre":
				b.WriteString("\n\n```\n")
				walkChildren(n, walk, true)
				b.WriteString("\n```\n")
				return
			case "code":
				if !pre {
					if txt := textOf(n); txt != "" {
						b.WriteString("`" + txt + "`")
					}
					return
				}
			case "blockquote":
				b.WriteString("\n\n> ")
				walkChildren(n, walk, pre)
				b.WriteString("\n")
				return
			case "strong", "b":
				if txt := textOf(n); txt != "" {
					b.WriteString("**" + txt + "**")
				}
				return
			case "em", "i":
				if txt := textOf(n); txt != "" {
					b.WriteString("*" + txt + "*")
				}
				return
			case "td", "th":
				walkChildren(n, walk, pre)
				b.WriteString(" · ")
				return
			}
			walkChildren(n, walk, pre)
		case html.TextNode:
			if pre {
				b.WriteString(n.Data)
				return
			}
			raw := n.Data
			collapsed := strings.Join(strings.Fields(raw), " ")
			if collapsed == "" {
				return
			}
			// Preserve word boundaries the whitespace collapse would eat:
			// " con" after </strong> must keep its leading space.
			if startsWithSpace(raw) && !endsWithBoundary(b.String()) {
				b.WriteString(" ")
			}
			b.WriteString(collapsed)
			if endsWithSpace(raw) {
				b.WriteString(" ")
			}
		default:
			walkChildren(n, walk, pre)
		}
	}
	walk(root, false)
	return title, tidyMarkdown(b.String())
}

func walkChildren(n *html.Node, walk func(*html.Node, bool), pre bool) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, pre)
	}
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var rec func(*html.Node)
	rec = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			rec(c)
		}
	}
	rec(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func startsWithSpace(s string) bool {
	return s != "" && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r')
}

func endsWithSpace(s string) bool {
	if s == "" {
		return false
	}
	c := s[len(s)-1]
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// endsWithBoundary reports whether the builder already ends at a natural
// word boundary (whitespace or nothing yet).
func endsWithBoundary(s string) bool {
	return s == "" || endsWithSpace(s)
}

// tidyMarkdown collapses 3+ blank lines and trims trailing spaces.
func tidyMarkdown(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	blank := 0
	for _, l := range lines {
		l = strings.TrimRight(l, " \t")
		if strings.TrimSpace(l) == "" {
			blank++
			if blank > 1 {
				continue
			}
			out = append(out, "")
			continue
		}
		blank = 0
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
