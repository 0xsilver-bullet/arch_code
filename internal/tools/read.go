package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/openai/openai-go/v3"
)

const (
	readMaxBytes = 50 * 1024 // 50 KB
	readMaxLines = 2000
)

type ReadParams struct {
	Path      string `json:"path"`
	StartLine *int   `json:"startLine,omitempty"`
	EndLine   *int   `json:"endLine,omitempty"`
}

func ExecuteRead(cwd string, p ReadParams) string {
	absPath := p.Path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(cwd, absPath)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Sprintf("Error: cannot read file %q: %v", p.Path, err)
	}

	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)
	startLine, endLine := 1, totalLines

	if p.StartLine != nil && *p.StartLine >= 1 {
		startLine = *p.StartLine
	}
	if p.EndLine != nil && *p.EndLine <= totalLines {
		endLine = *p.EndLine
	}
	if startLine > totalLines {
		startLine = totalLines
	}
	if endLine < startLine {
		endLine = startLine
	}

	lines = lines[startLine-1 : endLine]

	var sb strings.Builder
	byteCount, lineCount := 0, 0
	truncated := false

	for _, line := range lines {
		lb := len(line) + 1
		if lineCount >= readMaxLines || byteCount+lb > readMaxBytes {
			truncated = true
			break
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
		byteCount += lb
		lineCount++
	}

	result := sb.String()
	if truncated {
		result += fmt.Sprintf(
			"\n[Truncated: 50KB limit. %d lines not shown. Use startLine/endLine to read specific sections.]",
			len(lines)-lineCount,
		)
	}
	RecordRead(absPath)
	return result
}

var ReadTool = openai.ChatCompletionToolUnionParam{
	OfFunction: &openai.ChatCompletionFunctionToolParam{
		Function: openai.FunctionDefinitionParam{
			Name:        "read",
			Description: openai.String("Read file contents"),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Path to the file to read. Relative paths are resolved from the current working directory.",
					},
					"startLine": map[string]any{
						"type":        "integer",
						"description": "First line to return (1-based). Omit to start from the beginning.",
					},
					"endLine": map[string]any{
						"type":        "integer",
						"description": "Last line to return (1-based, inclusive). Omit to read to end of file.",
					},
				},
				"required": []string{"path"},
			},
		},
	},
}
