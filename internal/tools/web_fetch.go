package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"golang.org/x/net/html"
)

const BrowserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
const LargeContentThreshold = 50000 // 50KB

var multipleNewlinesRe = regexp.MustCompile(`\n{3,}`)

type WebFetchParams struct {
	URL string `json:"url"`
}

func removeNoisyElements(htmlContent string) string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return htmlContent
	}

	noisyTags := map[string]bool{
		"script":   true,
		"style":    true,
		"nav":      true,
		"header":   true,
		"footer":   true,
		"aside":    true,
		"noscript": true,
		"iframe":   true,
		"svg":      true,
	}

	var removeNodes func(*html.Node)
	removeNodes = func(n *html.Node) {
		var toRemove []*html.Node

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && noisyTags[c.Data] {
				toRemove = append(toRemove, c)
			} else {
				removeNodes(c)
			}
		}

		for _, node := range toRemove {
			n.RemoveChild(node)
		}
	}

	removeNodes(doc)

	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return htmlContent
	}

	return buf.String()
}

func cleanupMarkdown(content string) string {
	content = multipleNewlinesRe.ReplaceAllString(content, "\n\n")
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	content = strings.Join(lines, "\n")
	content = strings.TrimSpace(content)
	return content
}

func FormatJSON(content string) (string, error) {
	var data any
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func FetchURLAndConvert(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", BrowserUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("request failed with status code: %d", resp.StatusCode)
	}

	maxSize := int64(5 * 1024 * 1024) // 5MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	content := string(body)

	if !utf8.ValidString(content) {
		return "", fmt.Errorf("response content is not valid UTF-8")
	}

	contentType := resp.Header.Get("Content-Type")

	if strings.Contains(contentType, "text/html") {
		cleanedHTML := removeNoisyElements(content)
		converter := md.NewConverter("", true, nil)
		markdown, err := converter.ConvertString(cleanedHTML)
		if err != nil {
			return "", fmt.Errorf("failed to convert HTML to markdown: %w", err)
		}
		content = cleanupMarkdown(markdown)
	} else if strings.Contains(contentType, "application/json") || strings.Contains(contentType, "text/json") {
		formatted, err := FormatJSON(content)
		if err == nil {
			content = formatted
		}
	}

	return content, nil
}

func ExecuteWebFetch(cwd string, params WebFetchParams) string {
	if params.URL == "" {
		return "error: url is required"
	}

	ctx := context.Background()
	client := &http.Client{Timeout: 30 * time.Second}

	content, err := FetchURLAndConvert(ctx, client, params.URL)
	if err != nil {
		return fmt.Sprintf("error: Failed to fetch URL: %s", err)
	}

	if len(content) > LargeContentThreshold {
		tempFile, err := os.CreateTemp(cwd, "page-*.md")
		if err != nil {
			return fmt.Sprintf("error: Failed to create temporary file: %s", err)
		}
		tempFilePath := tempFile.Name()

		if _, err := tempFile.WriteString(content); err != nil {
			_ = tempFile.Close()
			return fmt.Sprintf("error: Failed to write content to file: %s", err)
		}
		if err := tempFile.Close(); err != nil {
			return fmt.Sprintf("error: Failed to close temporary file: %s", err)
		}

		return fmt.Sprintf("Fetched content from %s (large page)\n\nContent saved to: %s\n\nUse the view and grep tools to analyze this file.", params.URL, tempFilePath)
	}

	return fmt.Sprintf("Fetched content from %s:\n\n%s", params.URL, content)
}
