package xpath

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/antchfx/xpath"
)

// Limited set of tags for generation to increase match probability.
var htmlTags = []string{"div", "p", "span", "a", "b", "i", "table", "tr", "td"}

// Limited set of attribute names.
var htmlAttrs = []string{"id", "class", "href", "title", "style"}

// genTNode generates a random TNode tree resembling simple HTML.
func genTNode() *rapid.Generator[*TNode] {
	return rapid.Custom(func(t *rapid.T) *TNode {
		// Use rapid.Rec to handle recursion safely.
		return rapid.Rec(t, func(r *rapid.T) *TNode {
			// Decide node type: element or text. Bias towards elements initially.
			isElement := rapid.Bool().Draw(r, "isElement")
			if !isElement {
				// Generate a text node. Limit string complexity.
				text := rapid.String().Draw(r, "textData")
				return createNode(text, xpath.TextNode)
			}

			// Generate an element node.
			tag := rapid.SampledFrom(htmlTags).Draw(r, "tag")
			node := createNode(tag, xpath.ElementNode)

			// Add attributes sometimes.
			if rapid.Bool().Draw(r, "hasAttrs") {
				numAttrs := rapid.IntRange(0, 3).Draw(r, "numAttrs")
				for i := 0; i < numAttrs; i++ {
					attrName := rapid.SampledFrom(htmlAttrs).Draw(r, fmt.Sprintf("attrName%d", i))
					// Ensure unique attribute names for simplicity, though not strictly required by HTML/XML.
					// This simple generator might add duplicate attrs, which is fine for crash testing.
					attrVal := rapid.String().Draw(r, fmt.Sprintf("attrVal%d", i))
					node.addAttribute(attrName, attrVal)
				}
			}

			// Add children sometimes. Limit depth and breadth.
			depth := rapid.IntRange(0, 3).Draw(r, "depth") // Example depth limit
			if depth > 0 && rapid.Bool().Draw(r, "hasChildren") {
				numChildren := rapid.IntRange(1, 5).Draw(r, "numChildren")
				for i := 0; i < numChildren; i++ {
					// Recursively generate child node using the recursive generator 'r'.
					child := genTNode().Draw(r, fmt.Sprintf("child%d", i))
					// Add the generated child node using the new AddChild method.
					node.AddChild(child)
				}
			}

			return node
		})
	})
}

// genAxis generates a random XPath axis.
func genAxis() *rapid.Generator[string] {
	axes := []string{
		"child", "descendant", "parent", "ancestor", "following-sibling",
		"preceding-sibling", "following", "preceding", "attribute", "self",
		"descendant-or-self", "ancestor-or-self",
		// "namespace", // Deprecated and often unsupported
	}
	return rapid.SampledFrom(axes)
}

// genNodeTest generates a random XPath node test (name test or kind test).
func genNodeTest() *rapid.Generator[string] {
	return rapid.OneOf(
		// Name tests
		rapid.Just("*"),
		rapid.SampledFrom(htmlTags),
		// Kind tests
		rapid.Just("node()"),
		rapid.Just("text()"),
		rapid.Just("element()"),
		rapid.Just("attribute()"),
		// More specific kind tests (less likely to match simple generated docs)
		// rapid.Just("comment()"),
		// rapid.Just("processing-instruction()"),
	)
}

// genStep generates a single XPath step (axis::nodetest).
func genStep() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		axis := genAxis().Draw(t, "axis")
		nodeTest := genNodeTest().Draw(t, "nodeTest")
		// Abbreviated syntax for common cases
		if axis == "child" && nodeTest != "attribute()" { // Avoid child::attribute()
			return nodeTest // Abbreviated child axis
		}
		if axis == "attribute" && nodeTest != "element()" && nodeTest != "text()" && nodeTest != "node()" { // Avoid attribute::element() etc.
			if nodeTest == "attribute()" {
				return "@*" // Abbreviated attribute::*
			}
			return "@" + nodeTest // Abbreviated attribute axis
		}
		return axis + "::" + nodeTest
	})
}

// genRelativePathExpr generates a relative XPath expression (sequence of steps).
func genRelativePathExpr() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		steps := rapid.SliceOf(genStep()).Min(1).Max(4).Draw(t, "steps")
		// Join steps with / or //
		// For simplicity, just use / for now. // adds complexity.
		return strings.Join(steps, "/")
	})
}

// genXPathExpr generates a simple absolute XPath expression.
func genXPathExpr() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		// Start with / or // or relative path
		start := rapid.SampledFrom([]string{"/", "//", ""}).Draw(t, "start")
		if start == "" && rapid.Bool().Draw(t, "forceAbsolute") {
			// Ensure we don't generate empty expressions often
			start = "/"
		}
		if start == "/" || start == "//" {
			// Handle cases like "/" or "//" which might need a path following
			if rapid.Bool().Draw(t, "hasRelativePath") || start == "//" { // // needs a path
				relativePath := genRelativePathExpr().Draw(t, "relativePath")
				// Avoid "//" followed by empty relative path if genRelativePathExpr could return empty
				if relativePath == "" {
					relativePath = "node()" // Default to something simple
				}
				return start + relativePath
			}
			// Just "/"
			return "/"

		}
		// Relative path start
		return genRelativePathExpr().Draw(t, "relativePath")
	})

	// TODO: Add predicates, functions, operators, etc.
}

// TestPropertyXPathCrash checks if evaluating random XPath expressions on random documents causes panics.
func TestPropertyXPathCrash(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// 1. Generate a random document tree
		// Need to ensure the root is suitable for navigation (e.g., wrap in a document node?)
		// createNavigator expects a TNode root. Let's generate an element as root.
		rootNode := genTNode().Filter(func(n *TNode) bool { return n.Type == xpath.ElementNode }).Draw(t, "doc")
		// Wrap the root element in a document node? The tests seem to use element nodes directly as roots.
		// Let's stick with element root for now.

		// 2. Generate a random XPath expression string
		exprStr := genXPathExpr().Draw(t, "expr")
		t.Logf("Testing document: %s", nodeToString(rootNode)) // Helper to visualize doc
		t.Logf("Testing expression: %s", exprStr)

		// 3. Compile the expression
		// We expect panics to be caught by rapid.Check
		expr, err := xpath.Compile(exprStr)
		if err != nil {
			// Skip if compilation fails - we're looking for runtime crashes.
			// Or potentially log it as an interesting case (generator bug?).
			t.Skipf("Failed to compile generated expr %q: %v", exprStr, err)
			return
		}

		// 4. Create a navigator for the document
		nav := createNavigator(rootNode)

		// 5. Evaluate the expression - rapid will catch panics here.
		_ = expr.Evaluate(nav)

		// Optional: Also test Select/Iterate API if desired
		// iter := xpath.Select(nav, exprStr) // Assuming Select exists and takes string
		// for iter.MoveNext() {
		//     // Just iterate to trigger potential panics
		// }
	})
}

// Helper function to visualize the generated TNode tree (optional)
func nodeToString(node *TNode) string {
	var sb strings.Builder
	var printNode func(*TNode, int)
	printNode = func(n *TNode, indent int) {
		sb.WriteString(strings.Repeat("  ", indent))
		switch n.Type {
		case xpath.ElementNode:
			sb.WriteString("<" + n.Data)
			for _, attr := range n.Attr {
				sb.WriteString(fmt.Sprintf(" %s=%q", attr.Key, attr.Value))
			}
			if n.FirstChild == nil {
				sb.WriteString("/>\n")
			} else {
				sb.WriteString(">\n")
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					printNode(child, indent+1)
				}
				sb.WriteString(strings.Repeat("  ", indent))
				sb.WriteString("</" + n.Data + ">\n")
			}
		case xpath.TextNode:
			sb.WriteString(fmt.Sprintf("%q\n", n.Data))
		case xpath.CommentNode:
			sb.WriteString(fmt.Sprintf("<!--%s-->\n", n.Data))
		case xpath.DocumentNode:
			sb.WriteString("Document:\n")
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				printNode(child, indent+1)
			}
		default:
			sb.WriteString(fmt.Sprintf("Unknown<%d>%s\n", n.Type, n.Data))
		}
	}
	printNode(node, 0)
	return sb.String()
}
