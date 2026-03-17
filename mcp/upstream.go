package mcp

import (
	"embed"
	"encoding/json"
	"log/slog"
)

//go:embed *_tools.json
var toolsFS embed.FS

// DefaultUpstreamTools returns the embedded tool definitions for upstream MCP
// services (Attio, Granola, Notion). The snapshot is checked into the repo and updated
// periodically. Any file matching *_tools.json in this package is loaded.
func DefaultUpstreamTools() []UpstreamTool {
	entries, err := toolsFS.ReadDir(".")
	if err != nil {
		slog.Error("failed to read embedded tools", "error", err)
		return nil
	}
	var tools []UpstreamTool
	for _, e := range entries {
		data, err := toolsFS.ReadFile(e.Name())
		if err != nil {
			slog.Error("failed to read embedded tool file", "file", e.Name(), "error", err)
			continue
		}
		var t []UpstreamTool
		if err := json.Unmarshal(data, &t); err != nil {
			slog.Error("failed to parse embedded tool file", "file", e.Name(), "error", err)
			continue
		}
		tools = append(tools, t...)
	}
	return tools
}
