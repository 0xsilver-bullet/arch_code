package tools

import (
	"encoding/json"
	"fmt"
)

func ExecToolCall(name, args, cwd string) string {
	var result string

	switch name {
	case "read":
		var p ReadParams
		if err := json.Unmarshal([]byte(args), &p); err != nil {
			result = fmt.Sprintf("Error parsing arguments: %v", err)
		} else {
			result = ExecuteRead(cwd, p)
		}
	case "bash":
		var p BashParams
		if err := json.Unmarshal([]byte(args), &p); err != nil {
			result = fmt.Sprintf("Error parsing arguments: %v", err)
		} else {
			result = ExecuteBash(cwd, p)
		}
	case "write":
		var p WriteParams
		if err := json.Unmarshal([]byte(args), &p); err != nil {
			result = fmt.Sprintf("Error parsing arguments: %v", err)
		} else {
			result = ExecuteWrite(cwd, p)
		}
	case "edit":
		var p EditParams
		if err := json.Unmarshal([]byte(args), &p); err != nil {
			result = fmt.Sprintf("Error parsing arguments: %v", err)
		} else {
			result = ExecuteEdit(cwd, p)
		}
	case "multiedit":
		var p MultiEditParams
		if err := json.Unmarshal([]byte(args), &p); err != nil {
			result = fmt.Sprintf("Error parsing arguments: %v", err)
		} else {
			result = ExecuteMultiEdit(cwd, p)
		}
	case "web_search":
		var p WebSearchParams
		if err := json.Unmarshal([]byte(args), &p); err != nil {
			result = fmt.Sprintf("Error parsing arguments: %v", err)
		} else {
			result = ExecuteWebSearch(cwd, p)
		}
	case "web_fetch":
		var p WebFetchParams
		if err := json.Unmarshal([]byte(args), &p); err != nil {
			result = fmt.Sprintf("Error parsing arguments: %v", err)
		} else {
			result = ExecuteWebFetch(cwd, p)
		}
	default:
		result = fmt.Sprintf("Unknown tool: %s", name)
	}

	return result
}
