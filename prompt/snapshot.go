package prompt

import (
	"strings"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/go-xmldom"
)

// PruneSnapshot removes redundant information from the snapshot to reduce token usage.
// It removes:
// - The static datamodel definition (since runtime:datamodel has the actual values)
// - event:schema attributes (since they're in the function declarations)
// - All action/executable content elements while preserving state structure
// - runtime:actions section (since transitions are converted to LLM tools/functions)
func PruneSnapshot(doc xmldom.Document) {
	if doc == nil {
		return
	}

	root := doc.DocumentElement()
	if root == nil {
		return
	}

	// Remove the static <datamodel> element since we have runtime:datamodel
	datamodels := root.GetElementsByTagName("datamodel")
	for i := uint(0); i < datamodels.Length(); i++ {
		dm := datamodels.Item(i).(xmldom.Element)
		// Only remove if it's not in the runtime namespace
		if dm.NamespaceURI() != xmldom.DOMString(agentml.RuntimeNamespaceURI) {
			if parent := dm.ParentNode(); parent != nil {
				parent.RemoveChild(dm)
			}
		}
	}

	// Remove runtime:actions element since transitions are converted to LLM tools
	runtimeActions := root.GetElementsByTagNameNS(agentml.RuntimeNamespaceURI, "actions")
	for i := uint(0); i < runtimeActions.Length(); i++ {
		if elem, ok := runtimeActions.Item(i).(xmldom.Element); ok {
			if parent := elem.ParentNode(); parent != nil {
				parent.RemoveChild(elem)
			}
		}
	}

	// Remove schema attributes from all transition elements (since they're in the function declarations)
	transitions := root.GetElementsByTagName("transition")
	for i := uint(0); i < transitions.Length(); i++ {
		if elem, ok := transitions.Item(i).(xmldom.Element); ok {
			elem.RemoveAttribute("schema")
		}
	}

	// Strip out all action elements while preserving structure
	stripActionElements(root)
}

var (
	// Define elements that should be removed (executable content and action elements)
	removeElements = map[string]bool{
		// Executable content wrappers
		"onentry": true,
		"onexit":  true,

		// Core executable content
		"log":     true,
		"send":    true,
		"raise":   true,
		"cancel":  true,
		"assign":  true,
		"script":  true,
		"if":      true,
		"elseif":  true,
		"else":    true,
		"foreach": true,

		// Data model operations (except runtime:datamodel)
		"data":     true,
		"param":    true,
		"content":  true,
		"donedata": true,

		// External communication actions
		"finalize": true,
	}

	// Core structural elements to preserve
	structuralElements = map[string]bool{
		"agentml":     true,
		"scxml":       true,
		"state":       true,
		"parallel":    true,
		"transition":  true,
		"initial":     true,
		"final":       true,
		"history":     true,
		"invoke":      true, // Keep invoke structure but remove its finalize
		"description": true, // Keep agent description for context
	}
)

// stripActionElements removes all action/executable content elements while preserving structure
func stripActionElements(elem xmldom.Element) {
	if elem == nil {
		return
	}

	// Iterate through children and process them
	children := elem.ChildNodes()
	for i := int(children.Length()) - 1; i >= 0; i-- {
		child := children.Item(uint(i))

		if childElem, ok := child.(xmldom.Element); ok {
			localName := string(childElem.LocalName())

			// Check if this is a runtime:* element - always preserve these
			if childElem.NamespaceURI() == xmldom.DOMString(agentml.RuntimeNamespaceURI) {
				// Keep all runtime:* elements and their children
				continue
			}

			if removeElements[localName] {
				// Remove action elements entirely
				elem.RemoveChild(child)
			} else if localName == "transition" {
				// Keep transition element but remove its executable content children
				clearTransitionContent(childElem)
			} else if structuralElements[localName] {
				// For structural elements, recurse to process their children
				stripActionElements(childElem)
			} else {
				// Remove any unknown elements (might be custom namespace actions)
				// unless they're in a special namespace we want to preserve
				if childElem.NamespaceURI() == "" ||
					childElem.NamespaceURI() == "http://www.w3.org/2005/07/scxml" {
					elem.RemoveChild(child)
				} else {
					// Remove other namespace elements that aren't runtime:*
					elem.RemoveChild(child)
				}
			}
		}
	}
}

// clearTransitionContent removes all executable content from a transition while preserving the transition element itself
func clearTransitionContent(transition xmldom.Element) {
	if transition == nil {
		return
	}

	// Remove all children from transitions (they should only contain executable content)
	children := transition.ChildNodes()
	for i := int(children.Length()) - 1; i >= 0; i-- {
		child := children.Item(uint(i))
		transition.RemoveChild(child)
	}
}

// CompressXML removes unnecessary whitespace and formatting from XML to minimize tokens
func CompressXML(xml string) string {
	// Remove leading/trailing whitespace from each line
	lines := strings.Split(xml, "\n")
	var compressed []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			compressed = append(compressed, trimmed)
		}
	}

	// Join without newlines
	result := strings.Join(compressed, "")

	// Remove spaces between tags when safe
	result = strings.ReplaceAll(result, "> <", "><")

	return result
}
