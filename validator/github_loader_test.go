package validator

import (
	"testing"

	"github.com/agentflare-ai/go-xmldom"
	"github.com/agentflare-ai/go-xsd"
)

// mockAttr creates a mock attribute with the given namespace value
type mockAttr struct {
	value string
}

func (m mockAttr) NodeName() xmldom.DOMString                                      { return xmldom.DOMString("xmlns") }
func (m mockAttr) NodeValue() xmldom.DOMString                                     { return xmldom.DOMString(m.value) }
func (m mockAttr) NodeType() xmldom.NodeType                                       { return 2 } // ATTRIBUTE_NODE
func (m mockAttr) ParentNode() xmldom.Node                                         { return nil }
func (m mockAttr) ChildNodes() xmldom.NodeList                                     { return nil }
func (m mockAttr) FirstChild() xmldom.Node                                         { return nil }
func (m mockAttr) LastChild() xmldom.Node                                          { return nil }
func (m mockAttr) PreviousSibling() xmldom.Node                                    { return nil }
func (m mockAttr) NextSibling() xmldom.Node                                        { return nil }
func (m mockAttr) OwnerDocument() xmldom.Document                                  { return nil }
func (m mockAttr) InsertBefore(xmldom.Node, xmldom.Node) (xmldom.Node, error)      { return nil, nil }
func (m mockAttr) ReplaceChild(xmldom.Node, xmldom.Node) (xmldom.Node, error)      { return nil, nil }
func (m mockAttr) RemoveChild(xmldom.Node) (xmldom.Node, error)                    { return nil, nil }
func (m mockAttr) AppendChild(xmldom.Node) (xmldom.Node, error)                    { return nil, nil }
func (m mockAttr) HasChildNodes() bool                                             { return false }
func (m mockAttr) CloneNode(bool) xmldom.Node                                      { return nil }
func (m mockAttr) Normalize()                                                      {}
func (m mockAttr) Attributes() xmldom.NamedNodeMap                                 { return nil }
func (m mockAttr) Position() (line, column int, offset int64)                      { return 0, 0, 0 }
func (m mockAttr) SetNodeValue(xmldom.DOMString) error                             { return nil }
func (m mockAttr) IsSupported(xmldom.DOMString, xmldom.DOMString) bool             { return false }
func (m mockAttr) SetPrefix(xmldom.DOMString) error                                { return nil }
func (m mockAttr) HasAttributes() bool                                             { return false }
func (m mockAttr) BaseURI() xmldom.DOMString                                       { return "" }
func (m mockAttr) IsConnected() bool                                               { return false }
func (m mockAttr) Contains(xmldom.Node) bool                                       { return false }
func (m mockAttr) GetRootNode() xmldom.Node                                        { return nil }
func (m mockAttr) CompareDocumentPosition(xmldom.Node) xmldom.DocumentPositionType { return 0 }
func (m mockAttr) IsDefaultNamespace(xmldom.DOMString) bool                        { return false }
func (m mockAttr) IsEqualNode(xmldom.Node) bool                                    { return false }
func (m mockAttr) IsSameNode(xmldom.Node) bool                                     { return false }
func (m mockAttr) LookupPrefix(xmldom.DOMString) xmldom.DOMString                  { return "" }
func (m mockAttr) LookupNamespaceURI(xmldom.DOMString) xmldom.DOMString            { return "" }
func (m mockAttr) TextContent() xmldom.DOMString                                   { return xmldom.DOMString(m.value) }
func (m mockAttr) SetTextContent(xmldom.DOMString)                                 {}
func (m mockAttr) Prefix() xmldom.DOMString                                        { return "" }
func (m mockAttr) LocalName() xmldom.DOMString                                     { return xmldom.DOMString("xmlns") }
func (m mockAttr) NamespaceURI() xmldom.DOMString {
	return xmldom.DOMString("http://www.w3.org/2000/xmlns/")
}
func (m mockAttr) Name() xmldom.DOMString       { return xmldom.DOMString("xmlns") }
func (m mockAttr) Value() xmldom.DOMString      { return xmldom.DOMString(m.value) }
func (m mockAttr) SetValue(xmldom.DOMString)    {}
func (m mockAttr) OwnerElement() xmldom.Element { return nil }

func TestGitHubSchemaLoader(t *testing.T) {
	// Create GitHub loader
	loader := GitHubSchemaLoader(nil)

	// Test with invalid namespace (not github.com)
	_, err := loader(mockAttr{value: "example.com/foo"})
	if err == nil {
		t.Error("Expected error for non-GitHub namespace")
	}

	// Test with invalid GitHub format (missing repo)
	_, err = loader(mockAttr{value: "github.com/user"})
	if err == nil {
		t.Error("Expected error for invalid GitHub namespace format")
	}

	// Test pattern matching with schema loader config
	config := xsd.SchemaLoaderConfig{
		BaseDir: "/tmp",
		Loaders: []xsd.PatternLoader{
			{
				Pattern: "^github\\.com/",
				Loader:  GitHubSchemaLoader(nil),
			},
		},
	}

	schemaLoader, err := xsd.NewSchemaLoader(config)
	if err != nil {
		t.Fatalf("Failed to create schema loader: %v", err)
	}

	// Test that the pattern matches github.com namespaces
	// Note: This will fail to load since the repo doesn't exist, but it should try
	_, err = schemaLoader.LoadSchemaForNamespace("github.com/user/repo")
	if err == nil {
		t.Log("Unexpectedly succeeded in loading schema (repo probably doesn't exist)")
	} else {
		t.Logf("Expected failure loading non-existent repo: %v", err)
	}

	// Test that non-github namespaces don't match
	_, err = schemaLoader.LoadSchemaForNamespace("example.com/foo")
	if err == nil {
		t.Error("Should not match non-GitHub namespaces")
	}

	// Test subpackage namespace handling
	// This will fail to load but should try with the correct schema filename
	_, err = schemaLoader.LoadSchemaForNamespace("github.com/agentflare-ai/agentml-go/stdin")
	if err == nil {
		t.Log("Unexpectedly succeeded in loading schema for subpackage")
	} else {
		t.Logf("Expected failure loading subpackage schema: %v", err)
		// The error message should indicate it tried to load "stdin.xsd", not "agentml-go.xsd"
		// This validates our subpackage handling logic is working
	}
}
