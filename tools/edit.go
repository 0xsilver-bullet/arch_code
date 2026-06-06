package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
)

type EditParams struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

func toUnixLineEndings(s string) (string, bool) {
	if strings.Contains(s, "\r\n") {
		return strings.ReplaceAll(s, "\r\n", "\n"), true
	}
	return s, false
}

func toWindowsLineEndings(s string) string {
	return strings.ReplaceAll(s, "\n", "\r\n")
}

func ExecuteEdit(cwd string, p EditParams) string {
	if p.FilePath == "" {
		return "Error: file_path is required"
	}

	filePath := p.FilePath
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(cwd, filePath)
	}

	switch {
	case p.OldString == "" && p.NewString == "":
		return "Error: old_string and new_string cannot both be empty"
	case p.OldString == "":
		return editCreateFile(filePath, p.NewString)
	case p.NewString == "":
		return editDeleteContent(filePath, p.OldString, p.ReplaceAll)
	default:
		return editReplaceContent(filePath, p.OldString, p.NewString, p.ReplaceAll)
	}
}

func editCreateFile(filePath, content string) string {
	if fi, err := os.Stat(filePath); err == nil {
		if fi.IsDir() {
			return fmt.Sprintf("Error: path is a directory, not a file: %s", filePath)
		}
		return fmt.Sprintf("Error: file already exists: %s", filePath)
	} else if !os.IsNotExist(err) {
		return fmt.Sprintf("Error: failed to access file: %v", err)
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Sprintf("Error: failed to create parent directories: %v", err)
	}

	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		return fmt.Sprintf("Error: failed to write file: %v", err)
	}

	RecordRead(filePath)
	return fmt.Sprintf("<result>\nFile created: %s\n</result>", filePath)
}

func editDeleteContent(filePath, oldString string, replaceAll bool) string {
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
	var newContent string
	if replaceAll {
		newContent = strings.ReplaceAll(oldContent, oldString, "")
		if newContent == oldContent {
			return "Error: old_string not found in file. Make sure it matches exactly, including whitespace and line breaks."
		}
	} else {
		idx := strings.Index(oldContent, oldString)
		if idx == -1 {
			return "Error: old_string not found in file. Make sure it matches exactly, including whitespace and line breaks."
		}
		if strings.LastIndex(oldContent, oldString) != idx {
			return "Error: old_string appears multiple times in the file. Please provide more context to ensure a unique match, or set replace_all to true."
		}
		newContent = oldContent[:idx] + oldContent[idx+len(oldString):]
	}

	out := newContent
	if isCrlf {
		out = toWindowsLineEndings(newContent)
	}
	if err := os.WriteFile(filePath, []byte(out), 0o644); err != nil {
		return fmt.Sprintf("Error: failed to write file: %v", err)
	}

	RecordRead(filePath)
	return fmt.Sprintf("<result>\nContent deleted from file: %s\n</result>", filePath)
}

func editReplaceContent(filePath, oldString, newString string, replaceAll bool) string {
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
	var newContent string
	if replaceAll {
		newContent = strings.ReplaceAll(oldContent, oldString, newString)
	} else {
		idx := strings.Index(oldContent, oldString)
		if idx == -1 {
			return "Error: old_string not found in file. Make sure it matches exactly, including whitespace and line breaks."
		}
		if strings.LastIndex(oldContent, oldString) != idx {
			return "Error: old_string appears multiple times in the file. Please provide more context to ensure a unique match, or set replace_all to true."
		}
		newContent = oldContent[:idx] + newString + oldContent[idx+len(oldString):]
	}

	if oldContent == newContent {
		return "Error: new content is the same as old content. No changes made."
	}

	out := newContent
	if isCrlf {
		out = toWindowsLineEndings(newContent)
	}
	if err := os.WriteFile(filePath, []byte(out), 0o644); err != nil {
		return fmt.Sprintf("Error: failed to write file: %v", err)
	}

	RecordRead(filePath)
	return fmt.Sprintf("<result>\nContent replaced in file: %s\n</result>", filePath)
}

var EditTool = openai.ChatCompletionToolUnionParam{
	OfFunction: &openai.ChatCompletionFunctionToolParam{
		Function: openai.FunctionDefinitionParam{
			Name:        "edit",
			Description: openai.String("Edit a file by exact find-and-replace; can also create or delete content. For renames/moves use bash. For large edits use write. Requires the file to have been read first (except when creating). old_string must be unique unless replace_all is true."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "The path to the file to modify. Relative paths are resolved from the current working directory.",
					},
					"old_string": map[string]any{
						"type":        "string",
						"description": "The text to replace. Empty string means create a new file (combined with new_string as the file contents).",
					},
					"new_string": map[string]any{
						"type":        "string",
						"description": "The text to replace it with. Empty string means delete the old_string from the file.",
					},
					"replace_all": map[string]any{
						"type":        "boolean",
						"description": "Replace all occurrences of old_string (default false). When false, old_string must appear exactly once.",
					},
				},
				"required": []string{"file_path", "old_string", "new_string"},
			},
		},
	},
}
