package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
)

type MultiEditOperation struct {
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type MultiEditParams struct {
	FilePath string               `json:"file_path"`
	Edits    []MultiEditOperation `json:"edits"`
}

type failedEdit struct {
	Index int
	Error string
}

func ExecuteMultiEdit(cwd string, p MultiEditParams) string {
	if p.FilePath == "" {
		return "Error: file_path is required"
	}
	if len(p.Edits) == 0 {
		return "Error: at least one edit operation is required"
	}

	for i, e := range p.Edits {
		if i > 0 && e.OldString == "" {
			return fmt.Sprintf("Error: edit %d: only the first edit can have empty old_string (for file creation)", i+1)
		}
	}

	filePath := p.FilePath
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(cwd, filePath)
	}

	if p.Edits[0].OldString == "" {
		return multiEditWithCreation(filePath, p.Edits)
	}
	return multiEditExistingFile(filePath, p.Edits)
}

func multiEditWithCreation(filePath string, edits []MultiEditOperation) string {
	if _, err := os.Stat(filePath); err == nil {
		return fmt.Sprintf("Error: file already exists: %s", filePath)
	} else if !os.IsNotExist(err) {
		return fmt.Sprintf("Error: failed to access file: %v", err)
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Sprintf("Error: failed to create parent directories: %v", err)
	}

	current := edits[0].NewString
	var failed []failedEdit
	for i := 1; i < len(edits); i++ {
		next, err := applyEditToContent(current, edits[i])
		if err != nil {
			failed = append(failed, failedEdit{Index: i + 1, Error: err.Error()})
			continue
		}
		current = next
	}

	if err := os.WriteFile(filePath, []byte(current), 0o644); err != nil {
		return fmt.Sprintf("Error: failed to write file: %v", err)
	}

	RecordRead(filePath)

	applied := len(edits) - len(failed)
	if len(failed) > 0 {
		return fmt.Sprintf(
			"<result>\nFile created with %d of %d edits: %s (%d edit(s) failed)\n%s\n</result>",
			applied, len(edits), filePath, len(failed), formatFailedEdits(failed),
		)
	}
	return fmt.Sprintf("<result>\nFile created with %d edits: %s\n</result>", len(edits), filePath)
}

func multiEditExistingFile(filePath string, edits []MultiEditOperation) string {
	fi, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("Error: file not found: %s", filePath)
		}
		return fmt.Sprintf("Error: failed to access file: %v", err)
	}
	if fi.IsDir() {
		return fmt.Sprintf("Error: path is a directory, not a file: %s", filePath)
	}

	lastRead := LastReadTime(filePath)
	if lastRead.IsZero() {
		return "Error: you must read the file before editing it. Use the read tool first."
	}
	modTime := fi.ModTime().Truncate(time.Second)
	if modTime.After(lastRead) {
		return fmt.Sprintf(
			"Error: file %s has been modified since it was last read (mod time: %s, last read: %s)",
			filePath, modTime.Format(time.RFC3339), lastRead.Format(time.RFC3339),
		)
	}

	raw, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Sprintf("Error: failed to read file: %v", err)
	}

	oldContent, isCrlf := toUnixLineEndings(string(raw))
	current := oldContent

	var failed []failedEdit
	for i, e := range edits {
		next, err := applyEditToContent(current, e)
		if err != nil {
			failed = append(failed, failedEdit{Index: i + 1, Error: err.Error()})
			continue
		}
		current = next
	}

	if oldContent == current {
		if len(failed) > 0 {
			return fmt.Sprintf(
				"Error: no changes made - all %d edit(s) failed\n%s",
				len(failed), formatFailedEdits(failed),
			)
		}
		return "Error: no changes made - all edits resulted in identical content"
	}

	out := current
	if isCrlf {
		out = toWindowsLineEndings(current)
	}
	if err := os.WriteFile(filePath, []byte(out), 0o644); err != nil {
		return fmt.Sprintf("Error: failed to write file: %v", err)
	}

	RecordRead(filePath)

	applied := len(edits) - len(failed)
	if len(failed) > 0 {
		return fmt.Sprintf(
			"<result>\nApplied %d of %d edits to file: %s (%d edit(s) failed)\n%s\n</result>",
			applied, len(edits), filePath, len(failed), formatFailedEdits(failed),
		)
	}
	return fmt.Sprintf("<result>\nApplied %d edits to file: %s\n</result>", len(edits), filePath)
}

func applyEditToContent(content string, e MultiEditOperation) (string, error) {
	if e.OldString == "" && e.NewString == "" {
		return content, nil
	}
	if e.OldString == "" {
		return "", fmt.Errorf("old_string cannot be empty for content replacement")
	}

	if e.ReplaceAll {
		if strings.Count(content, e.OldString) == 0 {
			return "", fmt.Errorf("old_string not found in content. Make sure it matches exactly, including whitespace and line breaks")
		}
		return strings.ReplaceAll(content, e.OldString, e.NewString), nil
	}

	idx := strings.Index(content, e.OldString)
	if idx == -1 {
		return "", fmt.Errorf("old_string not found in content. Make sure it matches exactly, including whitespace and line breaks")
	}
	if strings.LastIndex(content, e.OldString) != idx {
		return "", fmt.Errorf("old_string appears multiple times in the content. Please provide more context to ensure a unique match, or set replace_all to true")
	}
	return content[:idx] + e.NewString + content[idx+len(e.OldString):], nil
}

func formatFailedEdits(failed []failedEdit) string {
	var b strings.Builder
	for _, f := range failed {
		fmt.Fprintf(&b, "  - edit %d: %s\n", f.Index, f.Error)
	}
	return strings.TrimRight(b.String(), "\n")
}

var MultiEditTool = openai.ChatCompletionToolUnionParam{
	OfFunction: &openai.ChatCompletionFunctionToolParam{
		Function: openai.FunctionDefinitionParam{
			Name:        "multiedit",
			Description: openai.String("Apply multiple find-and-replace edits to a single file in one operation; edits run sequentially (each sees the result of the previous). Prefer over edit for multiple changes to the same file. Same exact-match rules as edit apply. If the first edit has empty old_string, the file is created with new_string as the initial content."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "The path to the file to modify. Relative paths are resolved from the current working directory.",
					},
					"edits": map[string]any{
						"type":        "array",
						"description": "Array of edit operations to perform sequentially on the file.",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"old_string": map[string]any{
									"type":        "string",
									"description": "The text to replace. Empty string is only allowed on the first edit (creates a new file with new_string as initial content).",
								},
								"new_string": map[string]any{
									"type":        "string",
									"description": "The text to replace it with.",
								},
								"replace_all": map[string]any{
									"type":        "boolean",
									"description": "Replace all occurrences of old_string (default false).",
								},
							},
							"required": []string{"old_string", "new_string"},
						},
					},
				},
				"required": []string{"file_path", "edits"},
			},
		},
	},
}
