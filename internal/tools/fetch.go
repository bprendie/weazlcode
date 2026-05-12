package tools

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type FetchURLTool struct {
	limits Limits
	client *http.Client
}

func NewFetchURLTool(limits Limits) *FetchURLTool {
	return &FetchURLTool{
		limits: limits,
		client: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
	}
}

func (t *FetchURLTool) Name() string { return "fetch_url" }
func (t *FetchURLTool) Description() string {
	return "Fetch an HTTP or HTTPS URL and return readable text"
}
func (t *FetchURLTool) SafetyLevel() SafetyLevel { return SafetyLevelSafe }
func (t *FetchURLTool) Parameters() []Parameter {
	return []Parameter{
		{Name: "url", Type: "string", Description: "HTTP or HTTPS URL to fetch", Required: true},
		{Name: "max_chars", Type: "number", Description: "Maximum characters to return", Required: false},
	}
}

func (t *FetchURLTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	raw, _ := params["url"].(string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("url parameter is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("only http and https URLs are allowed")
	}
	if err := rejectPrivateHost(u.Hostname()); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "weazlcode (https://github.com/bprendie/weazlcode)")
	req.Header.Set("Accept", "text/html,text/plain,application/xhtml+xml,application/json;q=0.8,*/*;q=0.2")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("%s returned %s", u.String(), resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(t.limits.outputLimit()*4)))
	if err != nil {
		return "", err
	}
	text := readableText(string(body), resp.Header.Get("Content-Type"))
	maxChars := intParam(params, "max_chars", t.limits.outputLimit(), 1, t.limits.outputLimit())
	if len(text) > maxChars {
		text = text[:maxChars] + fmt.Sprintf("\n\n[truncated: %d chars omitted]", len(text)-maxChars)
	}
	return fmt.Sprintf("Fetched: %s\n\n%s", resp.Request.URL.String(), strings.TrimSpace(text)), nil
}

func rejectPrivateHost(host string) error {
	if host == "" {
		return fmt.Errorf("host is required")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("refusing to fetch private or local address %s", host)
		}
	}
	return nil
}

var (
	scriptRe = regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</(script|style|noscript)>`)
	tagRe    = regexp.MustCompile(`(?s)<[^>]+>`)
	spaceRe  = regexp.MustCompile(`[ \t\r\f]+`)
	blankRe  = regexp.MustCompile(`\n{3,}`)
	titleRe  = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

func readableText(body, contentType string) string {
	if !strings.Contains(strings.ToLower(contentType), "html") {
		return body
	}
	title := ""
	if m := titleRe.FindStringSubmatch(body); len(m) == 2 {
		title = "Title: " + htmlUnescape(strings.TrimSpace(m[1])) + "\n\n"
	}
	body = scriptRe.ReplaceAllString(body, " ")
	body = strings.ReplaceAll(body, "</p>", "\n")
	body = strings.ReplaceAll(body, "<br>", "\n")
	body = strings.ReplaceAll(body, "<br/>", "\n")
	body = strings.ReplaceAll(body, "<br />", "\n")
	body = tagRe.ReplaceAllString(body, " ")
	body = htmlUnescape(body)
	body = spaceRe.ReplaceAllString(body, " ")
	body = strings.TrimSpace(blankRe.ReplaceAllString(body, "\n\n"))
	return title + body
}

func htmlUnescape(s string) string {
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
		"&nbsp;", " ",
	)
	return replacer.Replace(s)
}
