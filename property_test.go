package xpath

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// Limited set of tags for generation to increase match probability.
var htmlTags = []string{"div", "p", "span", "a", "b", "i", "table", "tr", "td"}

// Limited set of attribute names.
var htmlAttrs = []string{"id", "class", "href", "title", "style"}

// genTNode generates a random TNode tree resembling simple HTML.
// Declared at package level to allow recursive definition in init().
var genTNode *rapid.Generator[*TNode]

func init() {
	genTNode = rapid.Custom(func(t *rapid.T) *TNode {
		// Decide node type: element or text. Bias towards elements initially.
		// Limit recursion depth implicitly by reducing probability of elements at deeper levels,
		// or explicitly pass depth (more complex). Let's rely on rapid's size control for now.
		isElement := rapid.Bool().Draw(t, "isElement")
		if !isElement {
			// Generate a text node from a limited set.
			text := rapid.SampledFrom([]string{"", "foo", "bar"}).Draw(t, "textData")
			return createNode(text, TextNode)
		}

		// Generate an element node.
		tag := rapid.SampledFrom(htmlTags).Draw(t, "tag")
		node := createNode(tag, ElementNode)

		// Add attributes sometimes.
		if rapid.Bool().Draw(t, "hasAttrs") {
			numAttrs := rapid.IntRange(0, 3).Draw(t, "numAttrs")
			for i := 0; i < numAttrs; i++ {
				attrName := rapid.SampledFrom(htmlAttrs).Draw(t, fmt.Sprintf("attrName%d", i))
				// Ensure unique attribute names for simplicity, though not strictly required by HTML/XML.
				// This simple generator might add duplicate attrs, which is fine for crash testing.
				// Generate attribute value from a limited set.
				attrVal := rapid.SampledFrom([]string{"", "foo", "bar"}).Draw(t, fmt.Sprintf("attrVal%d", i))
				node.addAttribute(attrName, attrVal)
			}
		}

		// Add children sometimes. Limit depth and breadth via rapid's size control.
		if rapid.Bool().Draw(t, "hasChildren") {
			numChildren := rapid.IntRange(1, 5).Draw(t, "numChildren")
			for i := 0; i < numChildren; i++ {
				// Recursively generate child node using the already defined generator.
				child := genTNode.Draw(t, fmt.Sprintf("child%d", i))
				// Add the generated child node using the new AddChild method.
				node.AddChild(child)
			}
		}

		return node
	})
}

// genStringLiteral generates a random XPath string literal.
func genStringLiteral() *rapid.Generator[string] {
	// Using a limited set of simple strings for literals.
	// Ensure generated strings don't contain the quote character used.
	// Rapid's StringOf generator could be used for more complex strings,
	// but requires careful handling of escaping.
	return rapid.Custom(func(t *rapid.T) string {
		quote := rapid.SampledFrom([]string{"'", "\""}).Draw(t, "quote")
		// Simple content, avoiding the chosen quote. More robust generation
		// would handle escaping or filter characters.
		content := rapid.SampledFrom([]string{"", "foo", "bar", "test", "data"}).Draw(t, "content")
		return quote + content + quote
	})
}

// genNumberLiteral generates a random XPath number literal (integer for simplicity).
func genNumberLiteral() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		// Generate small integers, positive and negative.
		num := rapid.IntRange(-10, 100).Draw(t, "number")
		return fmt.Sprintf("%d", num)
	})
}

// Forward declaration for recursive use in generators.
var genRelativePathExpr *rapid.Generator[string]
var genPredicateContent *rapid.Generator[string]

func init() {
	// Define genRelativePathExpr here or ensure it's defined before use in genPredicateContent.
	// We'll define it later, but the forward declaration allows compilation.

	// genPredicateContent generates expressions suitable for inside [...].
	genPredicateContent = rapid.Custom(func(t *rapid.T) string {
		// Choose the type of predicate expression.
		// Weights can be adjusted based on desired frequency.
		return rapid.OneOf(
			// Index predicate: [1], [last()]
			rapid.Just("last()"),
			genNumberLiteral(),
			// Boolean predicate: [foo], [@id='bar'], [text()='foo'], [count(a)>0]
			genRelativePathExpr, // Represents existence check, e.g., [element]
			rapid.Custom(func(t *rapid.T) string { // Simple comparison: path = literal
				// Generate a simple path, often an attribute or text()
				lhsPath := rapid.OneOf(
					rapid.Just("text()"),
					rapid.Just("."),
					rapid.Custom(func(t *rapid.T) string { return "@" + rapid.SampledFrom(htmlAttrs).Draw(t, "attrName") }),
					rapid.SampledFrom(htmlTags), // Simple element name test
				).Draw(t, "lhsPath")

				// Add more comparison operators
				op := rapid.SampledFrom([]string{"=", "!=", "<", "<=", ">", ">="}).Draw(t, "compOp")

				// Generate a literal for the RHS (comparisons often involve numbers or strings)
				rhsLiteral := rapid.OneOf(genStringLiteral(), genNumberLiteral()).Draw(t, "rhsLiteral")

				return fmt.Sprintf("%s %s %s", lhsPath, op, rhsLiteral)
			}),
			rapid.Custom(func(t *rapid.T) string { // Function call predicate: [contains(., 'foo')]
				funcName := rapid.SampledFrom([]string{"contains", "starts-with"}).Draw(t, "funcName")
				// Argument 1: often context node or attribute/text
				arg1 := rapid.OneOf(
					rapid.Just("."),
					rapid.Just("text()"),
					rapid.Custom(func(t *rapid.T) string { return "@" + rapid.SampledFrom(htmlAttrs).Draw(t, "attrName") }),
				).Draw(t, "funcArg1")
				// Argument 2: string literal
				arg2 := genStringLiteral().Draw(t, "funcArg2")
				return fmt.Sprintf("%s(%s, %s)", funcName, arg1, arg2)
			}),
			// Add more complex predicates: position(), count(), boolean logic (and/or)
		).Draw(t, "predicateContent")
	})
}

// genPredicate generates a full predicate expression: '[' + content + ']'.
func genPredicate() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		content := genPredicateContent.Draw(t, "content")
		return "[" + content + "]"
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
		// element() and attribute() are XPath 2.0/3.0, not 1.0
		// rapid.Just("element()"),
		// rapid.Just("attribute()"),
		// More specific kind tests (less likely to match simple generated docs, and also XPath 1.0)
		rapid.Just("comment()"), // Enable comment() node test
		// rapid.Just("processing-instruction()"), // Often requires a name argument
	)
}

// genStep generates a single XPath step (axis::nodetest[predicate1][predicate2]...).
func genStep() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		axis := genAxis().Draw(t, "axis")
		nodeTest := genNodeTest().Draw(t, "nodeTest")
		stepBase := ""
		// Abbreviated syntax for common cases
		// Ensure axis and nodeTest are compatible before potentially abbreviating.
		canAbbreviateChild := axis == "child" && nodeTest != "attribute()" && nodeTest != "comment()" && nodeTest != "processing-instruction()"
		canAbbreviateAttr := axis == "attribute" && nodeTest != "element()" && nodeTest != "text()" && nodeTest != "node()" && nodeTest != "comment()" && nodeTest != "processing-instruction()"

		useAbbreviation := rapid.Bool().Draw(t, "useAbbreviation")

		if useAbbreviation && canAbbreviateChild {
			stepBase = nodeTest // Abbreviated child axis
		} else if useAbbreviation && canAbbreviateAttr {
			if nodeTest == "attribute()" || nodeTest == "*" {
				stepBase = "@*" // Abbreviated attribute::*
			} else {
				stepBase = "@" + nodeTest // Abbreviated attribute axis name test
			}
		} else {
			// Default to full syntax if abbreviation is not chosen or not applicable
			stepBase = axis + "::" + nodeTest
		}

		// Add predicates sometimes
		predicates := ""
		if rapid.Bool().Draw(t, "hasPredicates") {
			numPredicates := rapid.IntRange(1, 2).Draw(t, "numPredicates") // 1 or 2 predicates
			for i := 0; i < numPredicates; i++ {
				// Ensure genPredicateContent is initialized before drawing from genPredicate
				if genPredicateContent == nil {
					// This might happen if init order is tricky. Log or handle.
					// For now, assume init() worked correctly.
					t.Fatalf("genPredicateContent is nil, initialization order issue?")
				}
				predicates += genPredicate().Draw(t, fmt.Sprintf("predicate%d", i))
			}
		}

		return stepBase + predicates
	})
}

// genRelativePathExpr generates a relative XPath expression (sequence of steps).
// Now defined using the forward declaration.
func init() {
	// Assign the actual generator function to the forward-declared variable.
	// This breaks the init cycle dependency if genPredicateContent needs genRelativePathExpr.
	genRelativePathExpr = rapid.Custom(func(t *rapid.T) string {
		// Generate the number of steps first.
		numSteps := rapid.IntRange(1, 3).Draw(t, "numSteps") // Reduced max steps slightly
		steps := make([]string, numSteps)
		for i := 0; i < numSteps; i++ {
			steps[i] = genStep().Draw(t, fmt.Sprintf("step%d", i))
		}
		// Join steps with / or //
		separator := rapid.SampledFrom([]string{"/", "//"}).Draw(t, "separator")
		// Avoid leading // if the path starts relative, although parser might handle it.
		// Let's keep it simple: join all with the chosen separator.
		return strings.Join(steps, separator)
	})
}

// genSimpleFunctionCall generates calls to common XPath functions.
func genSimpleFunctionCall() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		// Select a function name from the list supported in the README
		funcName := rapid.SampledFrom([]string{
			// Core XPath 1.0
			"boolean", "ceiling", "concat", "contains", "count", "false", "floor",
			"last", "local-name", "name", "namespace-uri", "normalize-space",
			"not", "number", "position", "round", "starts-with", "string",
			"string-length", "substring", "substring-after", "substring-before",
			"sum", "translate", "true",
			// Added from README (potentially XPath 2.0+)
			"ends-with", "lower-case", "matches", "replace", "reverse", "string-join",
			// lang() is explicitly marked as unsupported (✗) in the README.
			// "lang",
		}).Draw(t, "funcName")

		// Generate arguments based on the function
		// This is simplified; a real implementation needs function signatures.
		args := ""
		numArgs := 0
		switch funcName {
		// Functions that can take 0 or 1 argument (node-set/path)
		case "string", "boolean", "number", "name", "namespace-uri", "local-name", "normalize-space":
			if rapid.Bool().Draw(t, "hasArg") {
				arg := rapid.OneOf(rapid.Just("."), genRelativePathExpr).Draw(t, "arg0")
				args = arg
				numArgs = 1
			}
		// count() and sum() MUST take exactly 1 argument (node-set)
		case "count", "sum":
			numArgs = 1
			// Argument must evaluate to a node-set.
			args = rapid.OneOf(rapid.Just("."), genRelativePathExpr).Draw(t, "arg0")
		case "concat": // 2+ arguments
			numArgs = rapid.IntRange(2, 4).Draw(t, "numConcatArgs")
			argList := make([]string, numArgs)
			for i := 0; i < numArgs; i++ {
				// Args are typically strings or expressions evaluating to strings
				argList[i] = rapid.OneOf(genStringLiteral(), genRelativePathExpr).Draw(t, fmt.Sprintf("concatArg%d", i))
			}
			args = strings.Join(argList, ", ")
		case "starts-with", "contains": // 2 arguments (string, string)
			numArgs = 2
			arg1 := rapid.OneOf(rapid.Just("."), genRelativePathExpr, genStringLiteral()).Draw(t, "strArg1")
			arg2 := genStringLiteral().Draw(t, "strArg2") // Second arg usually literal
			args = fmt.Sprintf("%s, %s", arg1, arg2)
		case "substring-before", "substring-after": // 2 arguments (string, string)
			numArgs = 2
			arg1 := rapid.OneOf(rapid.Just("."), genRelativePathExpr, genStringLiteral()).Draw(t, "strArg1")
			arg2 := genStringLiteral().Draw(t, "strArg2")
			args = fmt.Sprintf("%s, %s", arg1, arg2)
		case "substring": // 2 or 3 arguments (string, number, number?)
			numArgs = rapid.IntRange(2, 3).Draw(t, "numSubstringArgs")
			arg1 := rapid.OneOf(rapid.Just("."), genRelativePathExpr, genStringLiteral()).Draw(t, "strArg1")
			arg2 := genNumberLiteral().Draw(t, "numArg2")
			if numArgs == 3 {
				arg3 := genNumberLiteral().Draw(t, "numArg3")
				args = fmt.Sprintf("%s, %s, %s", arg1, arg2, arg3)
			} else {
				args = fmt.Sprintf("%s, %s", arg1, arg2)
			}
		case "string-length": // 1 argument (string) - Parser requires one argument.
			numArgs = 1
			// Argument needs to evaluate to string.
			args = rapid.OneOf(rapid.Just("."), genRelativePathExpr, genStringLiteral()).Draw(t, "strArg1")
		case "translate": // 3 arguments (string, string, string)
			numArgs = 3
			arg1 := rapid.OneOf(rapid.Just("."), genRelativePathExpr, genStringLiteral()).Draw(t, "strArg1")
			arg2 := genStringLiteral().Draw(t, "strArg2")
			arg3 := genStringLiteral().Draw(t, "strArg3")
			args = fmt.Sprintf("%s, %s, %s", arg1, arg2, arg3)
		case "not": // 1 argument (boolean)
			numArgs = 1
			// Argument needs to evaluate to boolean, e.g., a path, comparison, or function call
			// For simplicity, use a relative path or another simple function for now.
			arg := rapid.OneOf(genRelativePathExpr, rapid.Just("true()"), rapid.Just("false()")).Draw(t, "boolArg1")
			args = arg
		// case "lang": // Removed as it's unsupported by the library.
		// 	numArgs = 1
		// 	args = genStringLiteral().Draw(t, "langArg1")
		// Functions with no arguments:
		case "true", "false", "position", "last":
			numArgs = 0
		// Numeric functions often take node-sets:
		case "floor", "ceiling", "round":
			numArgs = 1
			// Argument needs to evaluate to number. Use path or number literal.
			args = rapid.OneOf(genRelativePathExpr, genNumberLiteral()).Draw(t, "numArg1")
		// Handle newly added functions (simplified argument generation)
		case "ends-with": // 2 args (string, string)
			numArgs = 2
			arg1 := rapid.OneOf(rapid.Just("."), genRelativePathExpr, genStringLiteral()).Draw(t, "strArg1")
			arg2 := genStringLiteral().Draw(t, "strArg2")
			args = fmt.Sprintf("%s, %s", arg1, arg2)
		case "lower-case": // 1 arg (string)
			numArgs = 1
			args = rapid.OneOf(rapid.Just("."), genRelativePathExpr, genStringLiteral()).Draw(t, "strArg1")
		case "matches": // 2-3 args (string, pattern, flags?) - Generate 2 args for simplicity
			numArgs = 2
			arg1 := rapid.OneOf(rapid.Just("."), genRelativePathExpr, genStringLiteral()).Draw(t, "strArg1")
			// Pattern is a string literal (regex) - keep simple
			arg2 := genStringLiteral().Draw(t, "regexPattern")
			args = fmt.Sprintf("%s, %s", arg1, arg2)
		case "replace": // 3 args (string, pattern, replacement)
			numArgs = 3
			arg1 := rapid.OneOf(rapid.Just("."), genRelativePathExpr, genStringLiteral()).Draw(t, "strArg1")
			arg2 := genStringLiteral().Draw(t, "regexPattern")
			arg3 := genStringLiteral().Draw(t, "replacementStr")
			args = fmt.Sprintf("%s, %s, %s", arg1, arg2, arg3)
		case "reverse": // 1 arg (node-set?) - Treat as string for simplicity? Spec unclear for 1.0 context.
			// Let's assume it takes a path expression.
			numArgs = 1
			args = genRelativePathExpr.Draw(t, "pathArg1")
		case "string-join": // 2 args (node-set?, separator)
			numArgs = 2
			// First arg is often path, second is string literal separator
			arg1 := genRelativePathExpr.Draw(t, "pathArg1")
			arg2 := genStringLiteral().Draw(t, "separatorStr")
			args = fmt.Sprintf("%s, %s", arg1, arg2)

		default:
			// Fallback for functions not explicitly handled (likely 0 args like true, false, position, last)
			// Check if the function *should* have args based on its name
			// For now, assume 0 args if not explicitly handled above.
			numArgs = 0
		}

		return fmt.Sprintf("%s(%s)", funcName, args)
	})
}

// genXPathExpr generates a simple absolute or relative XPath expression,
// potentially starting with '/', '//', or being a function call.
func genXPathExpr() *rapid.Generator[string] {
	// Use OneOf to decide the top-level structure
	return rapid.OneOf(
		// Option 1: Path expression (absolute or relative)
		rapid.Custom(func(t *rapid.T) string {
			// Start with / or // or relative path
			start := rapid.SampledFrom([]string{"/", "//", ""}).Draw(t, "start")
			if start == "" && rapid.Bool().Draw(t, "forceAbsolute") {
				// Ensure we don't generate empty expressions often
				start = "/"
			}

			// Generate the relative path part
			// Ensure genRelativePathExpr is initialized
			if genRelativePathExpr == nil {
				t.Fatalf("genRelativePathExpr is nil during genXPathExpr generation")
			}
			relativePath := genRelativePathExpr.Draw(t, "relativePath")

			// Handle edge cases like "/" or "//" which might need a path following
			if (start == "/" || start == "//") && relativePath == "" {
				// Avoid generating just "/" or "//" if relativePath is empty.
				// Append a simple node test if needed.
				relativePath = "node()"
			} else if start == "" && relativePath == "" {
				// Avoid generating completely empty string. Default to context node.
				return "."
			}

			// Combine start and relative path
			// Need to be careful about "//" followed by potentially empty relative path
			// or "/" followed by empty. The logic above tries to prevent empty relativePath
			// when start is / or //.
			return start + relativePath
		}),
		// Option 2: Top-level function call
		genSimpleFunctionCall(),
		// Option 3: Simple literal (less common as top-level expression but possible)
		// genStringLiteral(),
		// genNumberLiteral(),
		// TODO: Add UnionExpr ('|'), Operators (+, -, =, etc.) at the top level
	)

	// Original simpler implementation:
	/*
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
			// Just "/" is handled by the logic ensuring relativePath is non-empty if start is "/"
			// return "/" // This case is now covered above

		}
		// Relative path start is handled when start == ""
		// return genRelativePathExpr.Draw(t, "relativePath") // Covered by start + relativePath logic
	})
	*/
}

// TestPropertyXPathCrash checks if evaluating random XPath expressions on random documents causes panics
// or if the generator produces expressions that fail to compile.
func TestPropertyXPathCrash(t *testing.T) {
	t.Log("Starting TestPropertyXPathCrash...") // Log entry into the test function
	// Pass configuration options directly to Check
	rapid.Check(t, func(t *rapid.T) {
		// 1. Generate a random document tree
		// Need to ensure the root is suitable for navigation (e.g., wrap in a document node?)
		// createNavigator expects a TNode root. Let's generate an element as root.
		rootNode := genTNode.Filter(func(n *TNode) bool { return n.Type == ElementNode }).Draw(t, "doc")
		// Wrap the root element in a document node? The tests seem to use element nodes directly as roots.
		// Let's stick with element root for now.

		// 2. Generate a random XPath expression string
		exprStr := genXPathExpr().Draw(t, "expr")

		// t.Logf("Testing document: %s", nodeToString(rootNode)) // Original logging (removed)
		// t.Logf("Testing expression: %s", exprStr) // Original logging (removed)

		// 3. Compile the expression
		// We expect panics to be caught by rapid.Check
		expr, err := Compile(exprStr)
		if err != nil {
			// Fail the test if compilation fails. The generator should produce valid expressions.
			t.Fatalf("Generator produced invalid expr %q which failed to compile: %v\nDocument:\n%s", exprStr, err, nodeToString(rootNode))
			// No return needed, Fatalf exits the goroutine.
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
	}) // Remove the NumRuns option here
	t.Logf("TestPropertyXPathCrash finished.")
}

// Helper function to visualize the generated TNode tree (optional)
func nodeToString(node *TNode) string {
	var sb strings.Builder
	var printNode func(*TNode, int)
	printNode = func(n *TNode, indent int) {
		sb.WriteString(strings.Repeat("  ", indent))
		switch n.Type {
		case ElementNode:
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
		case TextNode:
			sb.WriteString(fmt.Sprintf("%q\n", n.Data))
		case CommentNode:
			sb.WriteString(fmt.Sprintf("<!--%s-->\n", n.Data))
		case RootNode: // Use RootNode constant
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
