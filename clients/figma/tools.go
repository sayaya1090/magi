// Package figma provides the local Figma development plugin bridge.
package figma

import "github.com/sayaya1090/magi/clients/appbridge"

const Instructions = "Work as a design assistant in the bound Figma file. Read the relevant selection or page before making small changes. Preserve typography, spacing, hierarchy, components and accessibility. Ask when design requirements are missing. Use only the provided design tools; do not run shell commands or edit source code. Report actual changes in the user's language."

func Tools() []appbridge.Tool {
	object := func(properties map[string]any, required ...string) map[string]any {
		schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema
	}
	return []appbridge.Tool{
		{Name: "figma_selection", Description: "Read captured current-page selection, at most 100 nodes; text capped at 4000 characters per node.", InputSchema: object(map[string]any{}), ReadOnly: true},
		{Name: "figma_page", Description: "Read first 100 immediate children of the bound current page. No recursive file scan.", InputSchema: object(map[string]any{}), ReadOnly: true},
		{Name: "figma_set_text", Description: "Replace one explicit current-page text node after loading fonts. Up to 20000 characters.", InputSchema: object(map[string]any{"id": map[string]any{"type": "string"}, "text": map[string]any{"type": "string", "maxLength": 20000}}, "id", "text")},
		{Name: "figma_create_frame", Description: "Create a named frame on the bound current page, dimensions 1 to 4096.", InputSchema: object(map[string]any{"name": map[string]any{"type": "string", "maxLength": 200}, "width": map[string]any{"type": "number", "minimum": 1, "maximum": 4096}, "height": map[string]any{"type": "number", "minimum": 1, "maximum": 4096}}, "name", "width", "height")},
	}
}
