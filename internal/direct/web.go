package direct

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// WebOptions configure web_fetch and web_search.
type WebOptions struct {
	// AllowHosts need no approval for web_fetch (exact host or ".suffix").
	AllowHosts []string
	// SearchURL is a search endpoint returning SearXNG-style JSON
	// ({"results":[{"title","url","content"}]}) with {query} replaced;
	// "" uses DuckDuckGo's HTML endpoint.
	SearchURL string
	// Timeout per request (0 = 20 s).
	Timeout time.Duration
	// Client overrides the HTTP client (tests).
	Client *http.Client
}

const maxWebBytes = 2 << 20

func (w WebOptions) client() *http.Client {
	if w.Client != nil {
		return w.Client
	}
	t := w.Timeout
	if t <= 0 {
		t = 20 * time.Second
	}
	return &http.Client{Timeout: t}
}

func (w WebOptions) hostAllowed(host string) bool {
	host = strings.ToLower(host)
	for _, h := range w.AllowHosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" {
			continue
		}
		if h == host || h == "*" || (strings.HasPrefix(h, ".") && strings.HasSuffix(host, h)) || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

func hostOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func webConsent(r *Runner, a map[string]any) (string, bool) {
	if r.Opts.Approval == ApprovalAuto {
		return "", false
	}
	u := str(a, "url")
	if u == "" {
		u = "search: " + str(a, "query")
	} else if r.Opts.Web.hostAllowed(hostOf(u)) {
		return "", false
	}
	return "Fetch from the network (the URL and anything in it leave this machine):\n\n  " + u, true
}

func webFetchTool() Tool {
	return Tool{
		Name:        "web_fetch",
		Description: "Fetch a URL (http or https) and return its text (HTML is reduced to readable text). With prompt, the page is read for you and only the answer is returned.",
		Schema: schema(map[string]any{
			"url":    prop("string", "The URL to fetch"),
			"prompt": prop("string", "Optional question to answer from the page instead of returning it"),
		}, "url"),
		Summary: func(a map[string]any) string { return "fetch " + oneLine(str(a, "url"), 100) },
		Consent: webConsent,
		Run: func(ctx context.Context, r *Runner, a map[string]any) (string, error) {
			raw := strings.TrimSpace(str(a, "url"))
			u, err := url.Parse(raw)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return "", errors.New("url must be an absolute http(s) URL")
			}
			text, err := fetchText(ctx, r.Opts.Web.client(), u.String())
			if err != nil {
				return "", err
			}
			if q := strings.TrimSpace(str(a, "prompt")); q != "" {
				return r.answerFrom(ctx, text, q)
			}
			return text, nil
		},
	}
}

func fetchText(ctx context.Context, c *http.Client, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "BoundedCode/direct (+https://github.com/akynte/boundedcode)")
	req.Header.Set("Accept", "text/html,application/json,text/plain,*/*")
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxWebBytes))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("http %d from %s", resp.StatusCode, u)
	}
	ct := resp.Header.Get("Content-Type")
	text := string(b)
	if strings.Contains(ct, "html") || strings.HasPrefix(strings.TrimSpace(text), "<") {
		text = htmlToText(text)
	}
	return clipOutput(strings.TrimSpace(text)), nil
}

var (
	reScript = regexp.MustCompile(`(?is)<(script|style|noscript|svg|head)[^>]*>.*?</\s*(script|style|noscript|svg|head)\s*>`)
	reBlock  = regexp.MustCompile(`(?i)</?(p|div|br|li|ul|ol|h[1-6]|tr|table|section|article|header|footer|pre|blockquote)[^>]*>`)
	reTag    = regexp.MustCompile(`<[^>]+>`)
	reSpace  = regexp.MustCompile(`[ \t\r\f]+`)
	reLines  = regexp.MustCompile(`\n{3,}`)
)

// htmlToText reduces HTML to readable text: scripts and styles dropped,
// block tags become line breaks, entities decoded.
func htmlToText(s string) string {
	s = reScript.ReplaceAllString(s, " ")
	s = reBlock.ReplaceAllString(s, "\n")
	s = reTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = reSpace.ReplaceAllString(s, " ")
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return reLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
}

// answerFrom asks the model a question about text (web_fetch with prompt),
// without tools and within the context budget.
func (r *Runner) answerFrom(ctx context.Context, text, q string) (string, error) {
	limit := r.contextTokens() * 3 / 2 // bytes, about half the window
	if len(text) > limit {
		text = text[:limit] + "\n… (page truncated)"
	}
	body := map[string]any{
		"messages": []map[string]any{
			{"role": "system", "content": "Answer the question from the page text only. Quote the relevant parts. Say so if the page does not answer it."},
			{"role": "user", "content": "Question: " + q + "\n\nPage:\n" + text},
		},
		"max_tokens": 1500,
	}
	status, resp, err := r.Gateway.Forward(ctx, "/v1/chat/completions", body)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(resp)
	if status != http.StatusOK {
		return "", fmt.Errorf("reading the page failed (http %d): %s", status, errMessage(raw))
	}
	m, err := parseCompletion(raw)
	if err != nil {
		return "", err
	}
	return m.Content, nil
}

func webSearchTool() Tool {
	return Tool{
		Name:        "web_search",
		Description: "Search the web and return the top results (title, URL, snippet). Use web_fetch to read a result.",
		Schema:      schema(map[string]any{"query": prop("string", "The search query")}, "query"),
		Summary:     func(a map[string]any) string { return "search " + oneLine(str(a, "query"), 80) },
		Consent:     webConsent,
		Run: func(ctx context.Context, r *Runner, a map[string]any) (string, error) {
			q := strings.TrimSpace(str(a, "query"))
			if q == "" {
				return "", errors.New("query is required")
			}
			c := r.Opts.Web.client()
			if tpl := r.Opts.Web.SearchURL; tpl != "" {
				return searchJSON(ctx, c, strings.ReplaceAll(tpl, "{query}", url.QueryEscape(q)))
			}
			return searchDDG(ctx, c, q)
		},
	}
}

func searchJSON(ctx context.Context, c *http.Client, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Results []struct{ Title, URL, Content string } `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxWebBytes)).Decode(&out); err != nil {
		return "", fmt.Errorf("search endpoint returned no JSON results: %w", err)
	}
	return formatResults(out.Results), nil
}

var ddgResult = regexp.MustCompile(`(?s)<a[^>]+class="result__a"[^>]+href="([^"]+)"[^>]*>(.*?)</a>.*?(?:<a[^>]+class="result__snippet"[^>]*>(.*?)</a>|<td[^>]*class="result__snippet"[^>]*>(.*?)</td>)?`)

func searchDDG(ctx context.Context, c *http.Client, q string) (string, error) {
	u := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(q)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; BoundedCode)")
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, maxWebBytes))
	var results []struct{ Title, URL, Content string }
	for _, m := range ddgResult.FindAllStringSubmatch(string(b), 10) {
		href := html.UnescapeString(m[1])
		if pu, err := url.Parse(href); err == nil {
			if real := pu.Query().Get("uddg"); real != "" {
				href = real
			}
		}
		results = append(results, struct{ Title, URL, Content string }{htmlToText(m[2]), href, htmlToText(m[3] + m[4])})
	}
	if len(results) == 0 {
		return "(no results, or the search page could not be parsed; try web_fetch on a known site)", nil
	}
	return formatResults(results), nil
}

func formatResults(rs []struct{ Title, URL, Content string }) string {
	var b strings.Builder
	for i, r := range rs {
		if i >= 8 {
			break
		}
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, strings.TrimSpace(r.Title), r.URL)
		if s := strings.TrimSpace(r.Content); s != "" {
			fmt.Fprintf(&b, "   %s\n", oneLine(s, 300))
		}
	}
	if b.Len() == 0 {
		return "(no results)"
	}
	return strings.TrimRight(b.String(), "\n")
}
