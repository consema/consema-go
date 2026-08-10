package hcl

import (
	"strings"

	"consema.dev/consema/document"
)

// This file implements the HCL expression AST (RFC 0014 §4.3-§4.6, §6,
// §8.1). An expression is a first-class native role: the AST retains the
// frozen grammar as a kind with ordered children and exact half-open
// raw-byte spans, and its exact source text is always derived from the
// span against the immutable source (RFC 0014 §6 double preservation).
//
// Structural equality (RFC 0014 §6) is recursive over kind and children:
// number equality is canonical-decimal equality, template equality is
// part-wise with exact literal text and structural
// interpolation/directive comparison, constructor equality is
// element-wise, and node identity and source spans are never part of value
// equality.
//
// The literal-complete boundary (RFC 0014 §8.1) is a purely syntactic
// predicate: no evaluation, no arithmetic folding, no context. Numbers
// normalize to canonical decimal by pure decimal string arithmetic — zero
// floating-point computation (hard gate 1).

// HclExpression is one expression AST node with its exact half-open
// raw-byte span; the exact source text is always derived from the span
// against the frozen source.
type HclExpression struct {
	kind HclExpressionKind
	span document.Span
}

// NewHclExpression creates one expression node from its kind and exact
// span.
func NewHclExpression(kind HclExpressionKind, span document.Span) HclExpression {
	return HclExpression{kind: kind, span: span}
}

// Kind returns the closed native expression kind.
func (e *HclExpression) Kind() HclExpressionKind { return e.kind }

// Span returns the exact source span, including all trivia, operators, and
// delimiters.
func (e *HclExpression) Span() document.Span { return e.span }

// Text derives the exact source text from the span. HCL is UTF-8 only, so
// the span slice of the decoded text is the exact original spelling; this
// is the double-preserved raw text that no re-encoding can reproduce (RFC
// 0014 §6). The caller supplies the decoded text of the bound snapshot.
func (e *HclExpression) Text(decoded string) string {
	return decoded[e.span.StartByte():e.span.EndByte()]
}

// Children returns the ordered direct child expressions in source order:
// binary operands, function-call arguments, interpolation and directive
// expressions, traversal index keys (including keys inside splat steps),
// constructor elements and object-entry keys and values, and
// for-expression collection/value/guard expressions. The returned slice
// must not be modified.
func (e *HclExpression) Children() []*HclExpression {
	var children []*HclExpression
	switch e.kind.tag {
	case exprNumber, exprBoolean, exprNull, exprVariableRef:
	case exprTemplate:
		for i := range e.kind.parts {
			collectTemplatePartChildren(&e.kind.parts[i], &children)
		}
	case exprFunctionCall:
		for i := range e.kind.callArgs {
			children = append(children, e.kind.callArgs[i].Expression())
		}
	case exprTraversal:
		for i := range e.kind.steps {
			step := &e.kind.steps[i]
			switch step.tag {
			case stepIndex:
				children = append(children, step.key)
			case stepAttrSplat, stepFullSplat:
				for j := range step.steps {
					inner := &step.steps[j]
					if inner.tag == stepIndex {
						children = append(children, inner.key)
					}
				}
			}
		}
	case exprUnary:
		children = append(children, e.kind.unaryOperand)
	case exprBinary:
		children = append(children, e.kind.binaryLhs, e.kind.binaryRhs)
	case exprConditional:
		children = append(children, e.kind.condition, e.kind.thenExpr, e.kind.elseExpr)
	case exprForTuple:
		children = append(children, e.kind.intro.Collection())
		children = append(children, e.kind.forValue)
		if e.kind.forCondition != nil {
			children = append(children, e.kind.forCondition)
		}
	case exprForObject:
		children = append(children, e.kind.intro.Collection())
		children = append(children, e.kind.forKey, e.kind.forValue)
		if e.kind.forCondition != nil {
			children = append(children, e.kind.forCondition)
		}
	case exprTuple:
		children = append(children, e.kind.elements...)
	case exprObject:
		for i := range e.kind.entries {
			entry := &e.kind.entries[i]
			switch entry.key.tag {
			case keyParen:
				children = append(children, entry.key.parenInner)
			case keyTemplate:
				for j := range entry.key.template.parts {
					collectTemplatePartChildren(&entry.key.template.parts[j], &children)
				}
			}
			children = append(children, entry.value)
		}
	case exprParen:
		children = append(children, e.kind.inner)
	}
	return children
}

// Equal reports structural equality against another expression (RFC 0014
// §6): recursive over kind and children, number equality as
// canonical-decimal equality, template equality part-wise, constructor
// equality element-wise; node identity and source spans are never part of
// value equality.
func (e *HclExpression) Equal(other *HclExpression) bool {
	if e == nil || other == nil {
		return e == other
	}
	return e.kind.Equal(other.kind)
}

func collectTemplatePartChildren(part *HclTemplatePart, children *[]*HclExpression) {
	switch part.tag {
	case partLiteral:
	case partInterpolation:
		*children = append(*children, part.expression)
	case partDirective:
		switch part.directive.tag {
		case directiveIf:
			*children = append(*children, part.directive.condition)
		case directiveFor:
			*children = append(*children, part.directive.intro.Collection())
		}
	}
}

// hclExpressionKindTag is the closed expression-kind tag set.
type hclExpressionKindTag uint8

// The closed expression kind tags (RFC 0014 §4.3-§4.6).
const (
	exprNumber hclExpressionKindTag = iota
	exprBoolean
	exprNull
	exprTemplate
	exprFunctionCall
	exprVariableRef
	exprTraversal
	exprUnary
	exprBinary
	exprConditional
	exprForTuple
	exprForObject
	exprTuple
	exprObject
	exprParen
)

// HclExpressionKind is the closed native HCL expression kind (RFC 0014
// §4.3-§4.6).
//
// The variant set is closed by the frozen grammar. A quoted template and a
// heredoc are one kind: a heredoc is a template whose HeredocFacts are
// carried explicitly and whose content parts cover the heredoc body (RFC
// 0014 §4.4-§4.5, §6). Structural equality is recursive over kind and
// children and never includes source spans.
type HclExpressionKind struct {
	tag hclExpressionKindTag

	// Number payload.
	number HclNumber
	// Boolean payload.
	boolean bool

	// Template payload.
	parts   []HclTemplatePart
	heredoc *HeredocFacts

	// FunctionCall payload.
	callName     string
	callNameSpan document.Span
	callArgs     []HclCallArg

	// VariableRef payload.
	variableName string

	// Traversal payload.
	root  HclTraversalRoot
	steps []HclTraversalStep

	// Unary payload.
	unaryOp      UnaryOp
	unaryOperand *HclExpression

	// Binary payload.
	binaryOp  BinaryOp
	binaryLhs *HclExpression
	binaryRhs *HclExpression

	// Conditional payload.
	condition *HclExpression
	thenExpr  *HclExpression
	elseExpr  *HclExpression

	// For-expression payload.
	intro        HclForIntro
	forValue     *HclExpression
	forKey       *HclExpression
	forCondition *HclExpression
	grouping     bool

	// Tuple payload.
	elements []*HclExpression

	// Object payload.
	entries []HclObjectEntry

	// Paren payload.
	inner *HclExpression
}

// NewNumberKind creates a number-literal kind.
func NewNumberKind(number HclNumber) HclExpressionKind {
	return HclExpressionKind{tag: exprNumber, number: number}
}

// NewBooleanKind creates a `true`/`false` keyword-literal kind.
func NewBooleanKind(value bool) HclExpressionKind {
	return HclExpressionKind{tag: exprBoolean, boolean: value}
}

// NewNullKind creates the `null` literal kind.
func NewNullKind() HclExpressionKind { return HclExpressionKind{tag: exprNull} }

// NewTemplateKind creates a quoted-template or heredoc kind with ordered
// parts and optional heredoc facts.
func NewTemplateKind(parts []HclTemplatePart, heredoc *HeredocFacts) HclExpressionKind {
	return HclExpressionKind{tag: exprTemplate, parts: parts, heredoc: heredoc}
}

// NewFunctionCallKind creates a function-call kind; the name is a plain
// identifier only — the namespaced `foo::bar()` form is a grammar error
// (RFC 0014 §4.3, §12 D-6).
func NewFunctionCallKind(name string, nameSpan document.Span, args []HclCallArg) HclExpressionKind {
	return HclExpressionKind{tag: exprFunctionCall, callName: name, callNameSpan: nameSpan, callArgs: args}
}

// NewVariableRefKind creates a variable-reference kind (a traversal root
// with no steps, RFC 0014 §4.1, §4.3).
func NewVariableRefKind(name string) HclExpressionKind {
	return HclExpressionKind{tag: exprVariableRef, variableName: name}
}

// NewTraversalKind creates a static traversal kind: a root followed by
// attribute, index, and splat steps (RFC 0014 §4.1, §4.3).
func NewTraversalKind(root HclTraversalRoot, steps []HclTraversalStep) HclExpressionKind {
	return HclExpressionKind{tag: exprTraversal, root: root, steps: steps}
}

// NewUnaryKind creates a unary-operation kind; only `-` and `!` exist, and
// unary operators bind at the term layer above every binary operator (RFC
// 0014 §4.3).
func NewUnaryKind(op UnaryOp, operand *HclExpression) HclExpressionKind {
	return HclExpressionKind{tag: exprUnary, unaryOp: op, unaryOperand: operand}
}

// NewBinaryKind creates a left-associative binary-operation kind (RFC 0014
// §4.3).
func NewBinaryKind(op BinaryOp, lhs, rhs *HclExpression) HclExpressionKind {
	return HclExpressionKind{tag: exprBinary, binaryOp: op, binaryLhs: lhs, binaryRhs: rhs}
}

// NewConditionalKind creates the conditional `condition ? then : else`
// production (RFC 0014 §4.3).
func NewConditionalKind(condition, thenExpr, elseExpr *HclExpression) HclExpressionKind {
	return HclExpressionKind{tag: exprConditional, condition: condition, thenExpr: thenExpr, elseExpr: elseExpr}
}

// NewForTupleKind creates a tuple for-expression kind; no iteration is
// ever performed (RFC 0014 §4.6, §6).
func NewForTupleKind(intro HclForIntro, value *HclExpression, condition *HclExpression) HclExpressionKind {
	return HclExpressionKind{tag: exprForTuple, intro: intro, forValue: value, forCondition: condition}
}

// NewForObjectKind creates an object for-expression kind; the `...`
// grouping marker is a source fact (RFC 0014 §4.6, §6).
func NewForObjectKind(intro HclForIntro, key, value *HclExpression, grouping bool, condition *HclExpression) HclExpressionKind {
	return HclExpressionKind{tag: exprForObject, intro: intro, forKey: key, forValue: value,
		grouping: grouping, forCondition: condition}
}

// NewTupleKind creates a tuple-constructor kind (RFC 0014 §4.6).
func NewTupleKind(elements []*HclExpression) HclExpressionKind {
	return HclExpressionKind{tag: exprTuple, elements: elements}
}

// NewObjectKind creates an object-constructor kind; entries are ordered
// and duplicate keys are preserved, never collapsed (RFC 0014 §4.6, §6).
func NewObjectKind(entries []HclObjectEntry) HclExpressionKind {
	return HclExpressionKind{tag: exprObject, entries: entries}
}

// NewParenKind creates a parenthesized-expression kind (RFC 0014 §4.3).
func NewParenKind(inner *HclExpression) HclExpressionKind {
	return HclExpressionKind{tag: exprParen, inner: inner}
}

// Name returns the closed payload-free kind name (RFC 0014 §7.1
// `hcl.expression-kind-is@1`).
func (k HclExpressionKind) Name() HclExpressionKindName {
	switch k.tag {
	case exprNumber:
		return HclExpressionKindNameNumber
	case exprBoolean:
		return HclExpressionKindNameBoolean
	case exprNull:
		return HclExpressionKindNameNull
	case exprTemplate:
		return HclExpressionKindNameTemplate
	case exprFunctionCall:
		return HclExpressionKindNameFunctionCall
	case exprVariableRef:
		return HclExpressionKindNameVariableRef
	case exprTraversal:
		return HclExpressionKindNameTraversal
	case exprUnary:
		return HclExpressionKindNameUnary
	case exprBinary:
		return HclExpressionKindNameBinary
	case exprConditional:
		return HclExpressionKindNameConditional
	case exprForTuple:
		return HclExpressionKindNameForTuple
	case exprForObject:
		return HclExpressionKindNameForObject
	case exprTuple:
		return HclExpressionKindNameTuple
	case exprObject:
		return HclExpressionKindNameObject
	case exprParen:
		return HclExpressionKindNameParenthesized
	}
	return HclExpressionKindNameNumber
}

// Equal reports structural equality against another kind (RFC 0014 §6).
func (k HclExpressionKind) Equal(other HclExpressionKind) bool {
	if k.tag != other.tag {
		return false
	}
	switch k.tag {
	case exprNumber:
		return k.number.Equal(&other.number)
	case exprBoolean:
		return k.boolean == other.boolean
	case exprNull:
		return true
	case exprTemplate:
		return templatePartsEqual(k.parts, other.parts) && heredocFactsEqual(k.heredoc, other.heredoc)
	case exprFunctionCall:
		return k.callName == other.callName && callArgsEqual(k.callArgs, other.callArgs)
	case exprVariableRef:
		return k.variableName == other.variableName
	case exprTraversal:
		return k.root == other.root && traversalStepsEqual(k.steps, other.steps)
	case exprUnary:
		return k.unaryOp == other.unaryOp && k.unaryOperand.Equal(other.unaryOperand)
	case exprBinary:
		return k.binaryOp == other.binaryOp && k.binaryLhs.Equal(other.binaryLhs) &&
			k.binaryRhs.Equal(other.binaryRhs)
	case exprConditional:
		return k.condition.Equal(other.condition) && k.thenExpr.Equal(other.thenExpr) &&
			k.elseExpr.Equal(other.elseExpr)
	case exprForTuple:
		return k.intro.Equal(other.intro) && k.forValue.Equal(other.forValue) &&
			expressionsEqual(k.forCondition, other.forCondition)
	case exprForObject:
		return k.intro.Equal(other.intro) && k.forKey.Equal(other.forKey) &&
			k.forValue.Equal(other.forValue) && k.grouping == other.grouping &&
			expressionsEqual(k.forCondition, other.forCondition)
	case exprTuple:
		if len(k.elements) != len(other.elements) {
			return false
		}
		for i := range k.elements {
			if !k.elements[i].Equal(other.elements[i]) {
				return false
			}
		}
		return true
	case exprObject:
		if len(k.entries) != len(other.entries) {
			return false
		}
		for i := range k.entries {
			if !k.entries[i].Equal(other.entries[i]) {
				return false
			}
		}
		return true
	case exprParen:
		return k.inner.Equal(other.inner)
	}
	return false
}

func expressionsEqual(left, right *HclExpression) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Equal(right)
}

func templatePartsEqual(left, right []HclTemplatePart) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !left[i].Equal(right[i]) {
			return false
		}
	}
	return true
}

func heredocFactsEqual(left, right *HeredocFacts) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.mode == right.mode && left.marker == right.marker
}

func callArgsEqual(left, right []HclCallArg) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].expand != right[i].expand || !left[i].expression.Equal(right[i].expression) {
			return false
		}
	}
	return true
}

func traversalStepsEqual(left, right []HclTraversalStep) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !left[i].Equal(&right[i]) {
			return false
		}
	}
	return true
}

// HclExpressionKindName is the closed payload-free expression kind name set
// (RFC 0014 §7.1 `hcl.expression-kind-is@1`).
type HclExpressionKindName uint8

// The fifteen closed expression kind names.
const (
	// HclExpressionKindNameNumber is a decimal number literal.
	HclExpressionKindNameNumber HclExpressionKindName = iota
	// HclExpressionKindNameBoolean is a `true`/`false` keyword literal.
	HclExpressionKindNameBoolean
	// HclExpressionKindNameNull is the `null` literal.
	HclExpressionKindNameNull
	// HclExpressionKindNameTemplate is a quoted template or heredoc.
	HclExpressionKindNameTemplate
	// HclExpressionKindNameFunctionCall is a function call.
	HclExpressionKindNameFunctionCall
	// HclExpressionKindNameVariableRef is a variable reference (bare
	// traversal root).
	HclExpressionKindNameVariableRef
	// HclExpressionKindNameTraversal is a static traversal with steps.
	HclExpressionKindNameTraversal
	// HclExpressionKindNameUnary is a unary operation (`-`, `!`).
	HclExpressionKindNameUnary
	// HclExpressionKindNameBinary is a binary operation.
	HclExpressionKindNameBinary
	// HclExpressionKindNameConditional is the conditional `? :`.
	HclExpressionKindNameConditional
	// HclExpressionKindNameForTuple is a tuple for-expression.
	HclExpressionKindNameForTuple
	// HclExpressionKindNameForObject is an object for-expression.
	HclExpressionKindNameForObject
	// HclExpressionKindNameTuple is a tuple constructor.
	HclExpressionKindNameTuple
	// HclExpressionKindNameObject is an object constructor.
	HclExpressionKindNameObject
	// HclExpressionKindNameParenthesized is a parenthesized expression.
	HclExpressionKindNameParenthesized
)

// AsStr returns the stable kind spelling.
func (n HclExpressionKindName) AsStr() string {
	switch n {
	case HclExpressionKindNameNumber:
		return "number"
	case HclExpressionKindNameBoolean:
		return "boolean"
	case HclExpressionKindNameNull:
		return "null"
	case HclExpressionKindNameTemplate:
		return "template"
	case HclExpressionKindNameFunctionCall:
		return "function-call"
	case HclExpressionKindNameVariableRef:
		return "variable-ref"
	case HclExpressionKindNameTraversal:
		return "traversal"
	case HclExpressionKindNameUnary:
		return "unary"
	case HclExpressionKindNameBinary:
		return "binary"
	case HclExpressionKindNameConditional:
		return "conditional"
	case HclExpressionKindNameForTuple:
		return "for-tuple"
	case HclExpressionKindNameForObject:
		return "for-object"
	case HclExpressionKindNameTuple:
		return "tuple"
	case HclExpressionKindNameObject:
		return "object"
	case HclExpressionKindNameParenthesized:
		return "parenthesized"
	}
	return "number"
}

// HclExpressionKindNameFromName resolves one stable kind spelling.
func HclExpressionKindNameFromName(name string) (HclExpressionKindName, bool) {
	switch name {
	case "number":
		return HclExpressionKindNameNumber, true
	case "boolean":
		return HclExpressionKindNameBoolean, true
	case "null":
		return HclExpressionKindNameNull, true
	case "template":
		return HclExpressionKindNameTemplate, true
	case "function-call":
		return HclExpressionKindNameFunctionCall, true
	case "variable-ref":
		return HclExpressionKindNameVariableRef, true
	case "traversal":
		return HclExpressionKindNameTraversal, true
	case "unary":
		return HclExpressionKindNameUnary, true
	case "binary":
		return HclExpressionKindNameBinary, true
	case "conditional":
		return HclExpressionKindNameConditional, true
	case "for-tuple":
		return HclExpressionKindNameForTuple, true
	case "for-object":
		return HclExpressionKindNameForObject, true
	case "tuple":
		return HclExpressionKindNameTuple, true
	case "object":
		return HclExpressionKindNameObject, true
	case "parenthesized":
		return HclExpressionKindNameParenthesized, true
	}
	return 0, false
}

// HclNumber is the exact decimal number literal: source spelling plus
// canonical value (RFC 0014 §4.1, §6, §8).
//
// The grammar is decimal only — `decimal+ ("." decimal+)? (expmark
// decimal+)?` with `expmark = ("e" | "E") ("+" | "-")?` — with no leading
// sign, no hexadecimal/octal/binary form, and no underscore separators.
// The canonical decimal is the normalized pure-decimal spelling with no
// leading zeros, no trailing fraction zeros, and the exponent folded into
// the decimal point position (`0` represents zero). Numeric equality is
// canonical-decimal equality, so `1.50`, `1.5`, and `15e-1` compare equal
// as values while remaining distinct source facts (RFC 0014 §6).
type HclNumber struct {
	span      document.Span
	canonical string
}

// NewHclNumber creates a number from its exact span and canonical decimal
// spelling.
func NewHclNumber(span document.Span, canonical string) HclNumber {
	return HclNumber{span: span, canonical: canonical}
}

// HclNumberFromSpelling creates a number from its exact source spelling,
// computing the canonical decimal; the second result is false when the
// spelling is not a valid §4.1 number, its exponent does not fit the
// bounded canonical-decimal contract, or the canonical spelling would
// exceed the frozen max_number_digits digit budget (RFC 0014 §11).
func HclNumberFromSpelling(span document.Span, spelling string, maxDigits int) (HclNumber, bool) {
	canonical, ok := canonicalDecimalBounded(spelling, maxDigits)
	if !ok {
		return HclNumber{}, false
	}
	return HclNumber{span: span, canonical: canonical}, true
}

// Span returns the exact source span of the number spelling.
func (n *HclNumber) Span() document.Span { return n.span }

// CanonicalDecimal returns the canonical decimal spelling: no leading
// zeros, no trailing fraction zeros, exponent folded into the decimal
// point position, `"0"` for zero (RFC 0014 §9).
func (n *HclNumber) CanonicalDecimal() string { return n.canonical }

// Equal reports canonical-decimal equality (RFC 0014 §6).
func (n *HclNumber) Equal(other *HclNumber) bool {
	if n == nil || other == nil {
		return n == other
	}
	return n.canonical == other.canonical
}

// CanonicalDecimal normalizes one decimal number spelling to its canonical
// form by pure decimal string arithmetic — zero floating-point computation
// (hard gate 1). It returns false for a grammar violation or an exponent
// that does not fit the bounded representation; the exponent folding is
// bounded by the frozen max_number_digits digit budget of
// DefaultHclParseLimits (RFC 0014 §11).
func CanonicalDecimal(spelling string) (string, bool) {
	return canonicalDecimalBounded(spelling, DefaultHclParseLimits().MaxNumberDigits)
}

// canonicalDecimalBounded is the same pure-decimal fold as
// CanonicalDecimal, but the exponent folding is checked against the
// maxDigits budget before any zero-padding loop or allocation runs (RFC
// 0014 §11).
func canonicalDecimalBounded(spelling string, maxDigits int) (string, bool) {
	bytes := []byte(spelling)
	index := 0
	for index < len(bytes) && bytes[index] >= '0' && bytes[index] <= '9' {
		index++
	}
	integerLen := index
	if integerLen == 0 {
		return "", false
	}
	fractionLen := 0
	if index < len(bytes) && bytes[index] == '.' {
		index++
		fractionStart := index
		for index < len(bytes) && bytes[index] >= '0' && bytes[index] <= '9' {
			index++
		}
		fractionLen = index - fractionStart
		if fractionLen == 0 {
			return "", false
		}
	}
	var exponent int64
	if index < len(bytes) && (bytes[index] == 'e' || bytes[index] == 'E') {
		index++
		negative := false
		if index < len(bytes) && (bytes[index] == '+' || bytes[index] == '-') {
			negative = bytes[index] == '-'
			index++
		}
		exponentStart := index
		for index < len(bytes) && bytes[index] >= '0' && bytes[index] <= '9' {
			index++
		}
		if index == exponentStart {
			return "", false
		}
		magnitude, err := parseInt64(spelling[exponentStart:index])
		if err != nil {
			return "", false
		}
		if negative {
			exponent = -magnitude
		} else {
			exponent = magnitude
		}
	}
	if index != len(bytes) {
		return "", false
	}
	// The value is the concatenated digits with the decimal point after
	// `integerLen + exponent` digits.
	var digits strings.Builder
	digits.Grow(integerLen + fractionLen)
	digits.WriteString(spelling[:integerLen])
	if fractionLen > 0 {
		digits.WriteString(spelling[integerLen+1 : integerLen+1+fractionLen])
	}
	digitsText := digits.String()
	stripped := strings.TrimLeft(digitsText, "0")
	point := int64(integerLen) + exponent - int64(len(digitsText)-len(stripped))
	if point < -int64(1)<<62 || point > int64(1)<<62 {
		return "", false
	}
	if stripped == "" {
		return "0", true
	}
	var out strings.Builder
	if point <= 0 {
		zeros := int(-point)
		trimmed := strings.TrimRight(stripped, "0")
		if zeros+len(trimmed)+1 > maxDigits {
			return "", false
		}
		out.WriteString("0.")
		for i := 0; i < zeros; i++ {
			out.WriteByte('0')
		}
		out.WriteString(stripped)
		text := out.String()
		for len(text) > 2 && text[len(text)-1] == '0' {
			text = text[:len(text)-1]
		}
		return text, true
	}
	positive := int(point)
	if positive >= len(stripped) {
		if positive > maxDigits {
			return "", false
		}
		out.WriteString(stripped)
		for i := 0; i < positive-len(stripped); i++ {
			out.WriteByte('0')
		}
		return out.String(), true
	}
	out.WriteString(stripped[:positive])
	fraction := strings.TrimRight(stripped[positive:], "0")
	if fraction != "" {
		out.WriteByte('.')
		out.WriteString(fraction)
	}
	return out.String(), true
}

// UnaryOp is the unary operator set; exactly `-` and `!` exist, and unary
// `+` is a grammar error (RFC 0014 §4.3).
type UnaryOp uint8

// The two frozen unary operators.
const (
	// UnaryOpMinus is `-` negation.
	UnaryOpMinus UnaryOp = iota
	// UnaryOpNot is `!` logical not.
	UnaryOpNot
)

// AsStr returns the stable operator spelling.
func (o UnaryOp) AsStr() string {
	switch o {
	case UnaryOpMinus:
		return "-"
	case UnaryOpNot:
		return "!"
	}
	return "-"
}

// UnaryOpFromName resolves one operator spelling.
func UnaryOpFromName(name string) (UnaryOp, bool) {
	switch name {
	case "-":
		return UnaryOpMinus, true
	case "!":
		return UnaryOpNot, true
	}
	return 0, false
}

// BinaryOp is the binary operator set, frozen by the RFC 0014 §4.3
// precedence table.
type BinaryOp uint8

// The thirteen frozen binary operators.
const (
	// BinaryOpEqual is `==`.
	BinaryOpEqual BinaryOp = iota
	// BinaryOpNotEqual is `!=`.
	BinaryOpNotEqual
	// BinaryOpLess is `<`.
	BinaryOpLess
	// BinaryOpGreater is `>`.
	BinaryOpGreater
	// BinaryOpLessEqual is `<=`.
	BinaryOpLessEqual
	// BinaryOpGreaterEqual is `>=`.
	BinaryOpGreaterEqual
	// BinaryOpAdd is `+`.
	BinaryOpAdd
	// BinaryOpSubtract is `-`.
	BinaryOpSubtract
	// BinaryOpMultiply is `*`.
	BinaryOpMultiply
	// BinaryOpDivide is `/`.
	BinaryOpDivide
	// BinaryOpModulo is `%`.
	BinaryOpModulo
	// BinaryOpAnd is `&&`.
	BinaryOpAnd
	// BinaryOpOr is `||`.
	BinaryOpOr
)

// AsStr returns the stable operator spelling.
func (o BinaryOp) AsStr() string {
	switch o {
	case BinaryOpEqual:
		return "=="
	case BinaryOpNotEqual:
		return "!="
	case BinaryOpLess:
		return "<"
	case BinaryOpGreater:
		return ">"
	case BinaryOpLessEqual:
		return "<="
	case BinaryOpGreaterEqual:
		return ">="
	case BinaryOpAdd:
		return "+"
	case BinaryOpSubtract:
		return "-"
	case BinaryOpMultiply:
		return "*"
	case BinaryOpDivide:
		return "/"
	case BinaryOpModulo:
		return "%"
	case BinaryOpAnd:
		return "&&"
	case BinaryOpOr:
		return "||"
	}
	return "=="
}

// BinaryOpFromName resolves one operator spelling.
func BinaryOpFromName(name string) (BinaryOp, bool) {
	switch name {
	case "==":
		return BinaryOpEqual, true
	case "!=":
		return BinaryOpNotEqual, true
	case "<":
		return BinaryOpLess, true
	case ">":
		return BinaryOpGreater, true
	case "<=":
		return BinaryOpLessEqual, true
	case ">=":
		return BinaryOpGreaterEqual, true
	case "+":
		return BinaryOpAdd, true
	case "-":
		return BinaryOpSubtract, true
	case "*":
		return BinaryOpMultiply, true
	case "/":
		return BinaryOpDivide, true
	case "%":
		return BinaryOpModulo, true
	case "&&":
		return BinaryOpAnd, true
	case "||":
		return BinaryOpOr, true
	}
	return 0, false
}

// HclTraversalRoot is a traversal root; keyword spellings are dual-read
// roots, behaving as if they were references to variables of those names
// without ever being evaluated (RFC 0014 §4.1).
type HclTraversalRoot struct {
	tag   traversalRootTag
	name  string
	value bool
}

type traversalRootTag uint8

const (
	traversalRootVariable traversalRootTag = iota
	traversalRootBoolean
	traversalRootNull
)

// NewTraversalRootVariable creates a variable-name root.
func NewTraversalRootVariable(name string) HclTraversalRoot {
	return HclTraversalRoot{tag: traversalRootVariable, name: name}
}

// NewTraversalRootBoolean creates a `true`/`false` keyword root.
func NewTraversalRootBoolean(value bool) HclTraversalRoot {
	return HclTraversalRoot{tag: traversalRootBoolean, value: value}
}

// NewTraversalRootNull creates the `null` keyword root.
func NewTraversalRootNull() HclTraversalRoot {
	return HclTraversalRoot{tag: traversalRootNull}
}

func (r HclTraversalRoot) equal(other HclTraversalRoot) bool {
	return r.tag == other.tag && r.name == other.name && r.value == other.value
}

// traversalStepTag is the closed traversal-step tag set.
type traversalStepTag uint8

// The closed traversal step tags.
const (
	stepGetAttr traversalStepTag = iota
	stepIndex
	stepAttrSplat
	stepFullSplat
)

// HclTraversalStep is one static traversal step (RFC 0014 §4.3).
//
// Attribute steps admit identifiers only: the numeric form `foo.0` is a
// grammar error (RFC 0014 §12 D-5). Splat steps nest further steps: an
// attribute splat admits attribute steps only, a full splat admits
// attribute and index steps.
type HclTraversalStep struct {
	tag  traversalStepTag
	name string
	// Span of the whole step, including the dot or brackets.
	span  document.Span
	key   *HclExpression
	steps []HclTraversalStep
}

// NewGetAttrStep creates a `.Identifier` attribute step.
func NewGetAttrStep(name string, span document.Span) HclTraversalStep {
	return HclTraversalStep{tag: stepGetAttr, name: name, span: span}
}

// NewIndexStep creates a `[Expression]` index step.
func NewIndexStep(key *HclExpression, span document.Span) HclTraversalStep {
	return HclTraversalStep{tag: stepIndex, key: key, span: span}
}

// NewAttrSplatStep creates a `. * GetAttr*` attribute splat.
func NewAttrSplatStep(steps []HclTraversalStep) HclTraversalStep {
	return HclTraversalStep{tag: stepAttrSplat, steps: steps}
}

// NewFullSplatStep creates a `[ * ] (GetAttr | Index)*` full splat.
func NewFullSplatStep(steps []HclTraversalStep) HclTraversalStep {
	return HclTraversalStep{tag: stepFullSplat, steps: steps}
}

// Equal reports structural equality (RFC 0014 §6); spans are never part of
// value equality.
func (s *HclTraversalStep) Equal(other *HclTraversalStep) bool {
	if s == nil || other == nil {
		return s == other
	}
	if s.tag != other.tag {
		return false
	}
	switch s.tag {
	case stepGetAttr:
		return s.name == other.name
	case stepIndex:
		return s.key.Equal(other.key)
	case stepAttrSplat, stepFullSplat:
		return traversalStepsEqual(s.steps, other.steps)
	}
	return false
}

// hclTemplatePartTag is the closed template-part tag set.
type hclTemplatePartTag uint8

// The closed template part tags.
const (
	partLiteral hclTemplatePartTag = iota
	partInterpolation
	partDirective
)

// HclTemplatePart is one ordered template part (RFC 0014 §6).
//
// A literal part keeps its exact escape-decoded text; the raw escaped
// spelling remains a source fact of the part's span (RFC 0014 §4.4). The
// `~` strip markers of interpolations and directives are span-internal
// source facts, never applied. Consema never evaluates, so the
// single-interpolation unwrap is documented but never performed (RFC 0014
// §4.4).
type HclTemplatePart struct {
	tag hclTemplatePartTag
	// Exact span of the whole part, including delimiters and strip
	// markers.
	span document.Span
	// Literal payload: escape-decoded literal text.
	text string
	// Interpolation payload.
	expression *HclExpression
	// Directive payload.
	directive HclDirectiveKind
}

// NewLiteralTemplatePart creates a literal run part; escaped `$${` and
// `%%{` sequences decode to literal `${`/`%{` text and count as literal
// text (RFC 0014 §4.4).
func NewLiteralTemplatePart(span document.Span, text string) HclTemplatePart {
	return HclTemplatePart{tag: partLiteral, span: span, text: text}
}

// NewInterpolationTemplatePart creates an interpolation `${ Expression }`
// part with its optional `~` strip markers.
func NewInterpolationTemplatePart(span document.Span, expression *HclExpression) HclTemplatePart {
	return HclTemplatePart{tag: partInterpolation, span: span, expression: expression}
}

// NewDirectiveTemplatePart creates a directive part.
func NewDirectiveTemplatePart(span document.Span, kind HclDirectiveKind) HclTemplatePart {
	return HclTemplatePart{tag: partDirective, span: span, directive: kind}
}

// Span returns the exact span of the whole part.
func (p *HclTemplatePart) Span() document.Span { return p.span }

// IsLiteral reports whether this part is a literal run with no
// interpolation or directive.
func (p *HclTemplatePart) IsLiteral() bool { return p.tag == partLiteral }

// Text returns the escape-decoded literal text of a literal part.
func (p *HclTemplatePart) Text() string { return p.text }

// Expression returns the interpolated expression of an interpolation part.
func (p *HclTemplatePart) Expression() *HclExpression { return p.expression }

// Directive returns the directive kind of a directive part.
func (p *HclTemplatePart) Directive() HclDirectiveKind { return p.directive }

// Equal reports structural equality (RFC 0014 §6).
func (p *HclTemplatePart) Equal(other HclTemplatePart) bool {
	if p.tag != other.tag {
		return false
	}
	switch p.tag {
	case partLiteral:
		return p.text == other.text
	case partInterpolation:
		return p.expression.Equal(other.expression)
	case partDirective:
		return p.directive.Equal(other.directive)
	}
	return false
}

// HclDirectiveKind is one template directive kind (RFC 0014 §4.4).
//
// The single-identifier for-directive `%{ for x in list }` is valid —
// Consema freezes the pinned Go parser's behavior of reading a key only
// when a comma follows (RFC 0014 §4.4, §12 D-7).
type HclDirectiveKind struct {
	tag       hclDirectiveTag
	condition *HclExpression
	intro     *HclForIntro
}

type hclDirectiveTag uint8

// The closed directive tags.
const (
	directiveIf hclDirectiveTag = iota
	directiveElse
	directiveEndIf
	directiveFor
	directiveEndFor
)

// NewDirectiveIf creates a `%{ if Expression }` directive.
func NewDirectiveIf(condition *HclExpression) HclDirectiveKind {
	return HclDirectiveKind{tag: directiveIf, condition: condition}
}

// NewDirectiveElse creates a `%{ else }` directive.
func NewDirectiveElse() HclDirectiveKind { return HclDirectiveKind{tag: directiveElse} }

// NewDirectiveEndIf creates a `%{ endif }` directive.
func NewDirectiveEndIf() HclDirectiveKind { return HclDirectiveKind{tag: directiveEndIf} }

// NewDirectiveFor creates a `%{ for ... }` directive (key optional).
func NewDirectiveFor(intro HclForIntro) HclDirectiveKind {
	return HclDirectiveKind{tag: directiveFor, intro: &intro}
}

// NewDirectiveEndFor creates a `%{ endfor }` directive.
func NewDirectiveEndFor() HclDirectiveKind { return HclDirectiveKind{tag: directiveEndFor} }

// Condition returns the condition expression of an If directive.
func (d *HclDirectiveKind) Condition() *HclExpression { return d.condition }

// ForIntro returns the for introduction of a For directive.
func (d *HclDirectiveKind) ForIntro() *HclForIntro { return d.intro }

// Equal reports structural equality (RFC 0014 §6).
func (d *HclDirectiveKind) Equal(other HclDirectiveKind) bool {
	if d.tag != other.tag {
		return false
	}
	switch d.tag {
	case directiveIf:
		return d.condition.Equal(other.condition)
	case directiveFor:
		if d.intro == nil || other.intro == nil {
			return d.intro == other.intro
		}
		return d.intro.Equal(*other.intro)
	default:
		return true
	}
}

// HeredocMode is the heredoc mode fact: `<<` or `<<-` (RFC 0014 §4.5).
type HeredocMode uint8

// The two frozen heredoc modes.
const (
	// HeredocModePlain is `<<`: no indentation stripping.
	HeredocModePlain HeredocMode = iota
	// HeredocModeStripIndent is `<<-`: the literal value removes the
	// minimum number of leading spaces from each line's leading literal
	// text.
	HeredocModeStripIndent
)

// AsStr returns the stable mode spelling.
func (m HeredocMode) AsStr() string {
	switch m {
	case HeredocModePlain:
		return "<<"
	case HeredocModeStripIndent:
		return "<<-"
	}
	return "<<"
}

// HeredocFacts are the heredoc representation facts of one template (RFC
// 0014 §4.5, §6).
//
// The mode, marker spelling, marker span, and closing-line span are
// preserved representation facts; the `<<-` indentation stripping is
// performed only when the template's literal value is read, never
// destructively. Structural equality compares the mode and marker spelling
// only.
type HeredocFacts struct {
	mode        HeredocMode
	marker      string
	markerSpan  document.Span
	closingSpan *document.Span
}

// NewHeredocFacts creates heredoc facts for a formed heredoc.
func NewHeredocFacts(mode HeredocMode, marker string, markerSpan document.Span,
	closingSpan *document.Span) HeredocFacts {
	return HeredocFacts{mode: mode, marker: marker, markerSpan: markerSpan, closingSpan: closingSpan}
}

// Mode returns the heredoc mode (`<<` or `<<-`).
func (f *HeredocFacts) Mode() HeredocMode { return f.mode }

// Marker returns the bare identifier marker spelling.
func (f *HeredocFacts) Marker() string { return f.marker }

// MarkerSpan returns the exact span of the marker identifier.
func (f *HeredocFacts) MarkerSpan() document.Span { return f.markerSpan }

// ClosingSpan returns the exact span of the closing marker line, or nil
// for an unterminated heredoc (RFC 0014 §3).
func (f *HeredocFacts) ClosingSpan() *document.Span { return f.closingSpan }

// HclCallArg is one function-call argument with its expansion marker fact
// (RFC 0014 §4.3).
type HclCallArg struct {
	expression *HclExpression
	expand     bool
}

// NewHclCallArg creates one argument.
func NewHclCallArg(expression *HclExpression, expand bool) HclCallArg {
	return HclCallArg{expression: expression, expand: expand}
}

// Expression returns the argument expression.
func (a *HclCallArg) Expression() *HclExpression { return a.expression }

// Expand returns the `...` expansion marker fact; the marker may only
// appear on the final argument (a parser contract).
func (a *HclCallArg) Expand() bool { return a.expand }

// HclForIntro is the `for` introduction of a for-expression or
// for-directive (RFC 0014 §4.6).
type HclForIntro struct {
	key        *string
	value      string
	collection *HclExpression
	span       document.Span
}

// NewHclForIntro creates one introduction.
func NewHclForIntro(key *string, value string, collection *HclExpression,
	span document.Span) HclForIntro {
	return HclForIntro{key: key, value: value, collection: collection, span: span}
}

// Key returns the optional key identifier; nil is the single-identifier
// form (RFC 0014 §12 D-7).
func (i *HclForIntro) Key() *string { return i.key }

// Value returns the value identifier.
func (i *HclForIntro) Value() string { return i.value }

// Collection returns the collection expression.
func (i *HclForIntro) Collection() *HclExpression { return i.collection }

// Span returns the exact span of the whole introduction, including
// `for ... in ...:`.
func (i *HclForIntro) Span() document.Span { return i.span }

// Equal reports structural equality (RFC 0014 §6).
func (i *HclForIntro) Equal(other HclForIntro) bool {
	if (i.key == nil) != (other.key == nil) {
		return false
	}
	if i.key != nil && *i.key != *other.key {
		return false
	}
	return i.value == other.value && i.collection.Equal(other.collection)
}

// objectKeyTag is the closed object-key tag set.
type objectKeyTag uint8

// The closed object key tags.
const (
	keyIdentifier objectKeyTag = iota
	keyNumber
	keyTemplate
	keyParen
)

// HclObjectKey is one object-constructor key (RFC 0014 §4.6).
//
// The frozen key forms are an identifier (literal name), a number literal,
// a quoted template, or a parenthesized expression; any other expression
// key is a grammar error.
type HclObjectKey struct {
	tag        objectKeyTag
	name       string
	number     HclNumber
	template   HclTemplateKey
	parenInner *HclExpression
}

// NewIdentifierObjectKey creates a bare identifier key.
func NewIdentifierObjectKey(name string) HclObjectKey {
	return HclObjectKey{tag: keyIdentifier, name: name}
}

// NewNumberObjectKey creates a number-literal key.
func NewNumberObjectKey(number HclNumber) HclObjectKey {
	return HclObjectKey{tag: keyNumber, number: number}
}

// NewTemplateObjectKey creates a quoted-template key.
func NewTemplateObjectKey(template HclTemplateKey) HclObjectKey {
	return HclObjectKey{tag: keyTemplate, template: template}
}

// NewParenObjectKey creates a parenthesized-expression key.
func NewParenObjectKey(inner *HclExpression) HclObjectKey {
	return HclObjectKey{tag: keyParen, parenInner: inner}
}

// Equal reports structural equality (RFC 0014 §6).
func (k *HclObjectKey) Equal(other *HclObjectKey) bool {
	if k == nil || other == nil {
		return k == other
	}
	if k.tag != other.tag {
		return false
	}
	switch k.tag {
	case keyIdentifier:
		return k.name == other.name
	case keyNumber:
		return k.number.Equal(&other.number)
	case keyTemplate:
		return k.template.Equal(other.template)
	case keyParen:
		return k.parenInner.Equal(other.parenInner)
	}
	return false
}

// HclTemplateKey is a quoted-template object key (RFC 0014 §4.6).
type HclTemplateKey struct {
	parts []HclTemplatePart
	span  document.Span
}

// NewHclTemplateKey creates a quoted-template key from its ordered parts.
func NewHclTemplateKey(parts []HclTemplatePart, span document.Span) HclTemplateKey {
	return HclTemplateKey{parts: parts, span: span}
}

// Parts returns the ordered parts. The returned slice must not be
// modified.
func (k *HclTemplateKey) Parts() []HclTemplatePart { return k.parts }

// Span returns the exact span of the key, including the quotes.
func (k *HclTemplateKey) Span() document.Span { return k.span }

// Equal reports structural equality (RFC 0014 §6).
func (k *HclTemplateKey) Equal(other HclTemplateKey) bool {
	return templatePartsEqual(k.parts, other.parts)
}

// ObjectSeparator is the object-constructor key/value separator source fact
// (RFC 0014 §4.6).
type ObjectSeparator uint8

// The two frozen separators.
const (
	// ObjectSeparatorEquals is `=`.
	ObjectSeparatorEquals ObjectSeparator = iota
	// ObjectSeparatorColon is `:`.
	ObjectSeparatorColon
)

// AsStr returns the stable separator spelling.
func (s ObjectSeparator) AsStr() string {
	switch s {
	case ObjectSeparatorEquals:
		return "="
	case ObjectSeparatorColon:
		return ":"
	}
	return "="
}

// HclObjectEntry is one ordered object-constructor entry: key, separator,
// and value (RFC 0014 §4.6).
//
// Duplicate keys are preserved as ordered native facts with independent
// spans and are never collapsed (RFC 0014 §4.6, §6).
type HclObjectEntry struct {
	key       HclObjectKey
	separator ObjectSeparator
	value     *HclExpression
}

// NewHclObjectEntry creates one entry.
func NewHclObjectEntry(key HclObjectKey, separator ObjectSeparator, value *HclExpression) HclObjectEntry {
	return HclObjectEntry{key: key, separator: separator, value: value}
}

// Key returns the key identity.
func (e *HclObjectEntry) Key() *HclObjectKey { return &e.key }

// Separator returns the `=` or `:` separator source fact.
func (e *HclObjectEntry) Separator() ObjectSeparator { return e.separator }

// Value returns the value expression.
func (e *HclObjectEntry) Value() *HclExpression { return e.value }

// Equal reports structural equality (RFC 0014 §6).
func (e *HclObjectEntry) Equal(other HclObjectEntry) bool {
	return e.key.Equal(&other.key) && e.value.Equal(other.value)
}

// IsLiteralComplete reports whether an expression is literal-complete: its
// value is uniquely determined by the source text alone — no evaluation,
// no context (RFC 0014 §8.1).
//
// The boundary is deliberately purely syntactic: it is decidable without
// any evaluator, and no arithmetic is ever computed (hard gate 1). Exactly
// the following are literal-complete:
//
//   - a number literal (any decimal spelling);
//   - `true`, `false`, or `null`;
//   - a quoted or heredoc template containing zero interpolation and zero
//     directive sequences (escaped `$${`/`%%{` text counts as literal text);
//   - a tuple constructor whose elements are all literal-complete;
//   - an object constructor whose keys are identifiers, number literals,
//     quoted literal templates, or parenthesized literal-complete
//     expressions, and whose values are all literal-complete;
//   - a unary minus applied to a number literal;
//   - a parenthesized literal-complete expression.
//
// Everything else is derived.
func IsLiteralComplete(expression *HclExpression) bool {
	switch expression.kind.tag {
	case exprNumber, exprBoolean, exprNull:
		return true
	case exprTemplate:
		for i := range expression.kind.parts {
			if !expression.kind.parts[i].IsLiteral() {
				return false
			}
		}
		return true
	case exprTuple:
		for _, element := range expression.kind.elements {
			if !IsLiteralComplete(element) {
				return false
			}
		}
		return true
	case exprObject:
		for i := range expression.kind.entries {
			entry := &expression.kind.entries[i]
			if !IsLiteralComplete(entry.value) || !literalCompleteKey(&entry.key) {
				return false
			}
		}
		return true
	case exprUnary:
		return expression.kind.unaryOp == UnaryOpMinus &&
			expression.kind.unaryOperand.kind.tag == exprNumber
	case exprParen:
		return IsLiteralComplete(expression.kind.inner)
	}
	return false
}

func literalCompleteKey(key *HclObjectKey) bool {
	switch key.tag {
	case keyIdentifier, keyNumber:
		return true
	case keyTemplate:
		for i := range key.template.parts {
			if !key.template.parts[i].IsLiteral() {
				return false
			}
		}
		return true
	case keyParen:
		return IsLiteralComplete(key.parenInner)
	}
	return false
}

// NonLiteralExpressionError is the explicit-failure path of RFC 0014 §8: a
// literal-complete expression is required, and nothing converts, folds, or
// guesses.
type NonLiteralExpressionError struct{}

// Error implements error.
func (e NonLiteralExpressionError) Error() string { return "expression is not literal-complete" }

// Code returns the stable family code of the failure (RFC 0014 §8.1).
func (e NonLiteralExpressionError) Code() string {
	return "hcl.projection.non-literal-expression@1"
}

// LiteralValue extracts the typed literal value of a literal-complete
// expression (RFC 0014 §8.1-§8.2).
//
// The mapping freezes the `hcl.body@1` typed members: a canonical decimal
// without a fraction projects as an integer, one with a fraction as a real
// (`1e3` normalizes to `"1000"` and therefore projects as an integer);
// zero-interpolation templates project as strings with the exact code
// points, including the `<<-` indentation-stripped content; constructors
// project element-wise with duplicate object keys preserved in order; and
// a unary minus applies to a number literal only. A derived expression
// fails with NonLiteralExpressionError — never a null, empty, or converted
// result.
func LiteralValue(expression *HclExpression) (HclLiteralValue, error) {
	switch expression.kind.tag {
	case exprNumber:
		return numberLiteral(expression.kind.number.canonical), nil
	case exprBoolean:
		return HclLiteralValue{tag: literalBoolean, boolean: expression.kind.boolean}, nil
	case exprNull:
		return HclLiteralValue{tag: literalNull}, nil
	case exprTemplate:
		var text strings.Builder
		for i := range expression.kind.parts {
			part := &expression.kind.parts[i]
			switch part.tag {
			case partLiteral:
				text.WriteString(part.text)
			case partInterpolation, partDirective:
				return HclLiteralValue{}, NonLiteralExpressionError{}
			}
		}
		if expression.kind.heredoc != nil && expression.kind.heredoc.mode == HeredocModeStripIndent {
			return HclLiteralValue{tag: literalString, text: stripHeredocIndentation(text.String())}, nil
		}
		return HclLiteralValue{tag: literalString, text: text.String()}, nil
	case exprTuple:
		values := make([]HclLiteralValue, 0, len(expression.kind.elements))
		for _, element := range expression.kind.elements {
			value, err := LiteralValue(element)
			if err != nil {
				return HclLiteralValue{}, err
			}
			values = append(values, value)
		}
		return HclLiteralValue{tag: literalTuple, elements: values}, nil
	case exprObject:
		entries := make([]HclLiteralObjectEntry, 0, len(expression.kind.entries))
		for i := range expression.kind.entries {
			entry := &expression.kind.entries[i]
			key, err := literalKeyOf(entry.key)
			if err != nil {
				return HclLiteralValue{}, err
			}
			value, err := LiteralValue(entry.value)
			if err != nil {
				return HclLiteralValue{}, err
			}
			entries = append(entries, HclLiteralObjectEntry{key: key, value: value})
		}
		return HclLiteralValue{tag: literalObject, entries: entries}, nil
	case exprUnary:
		if expression.kind.unaryOp == UnaryOpMinus && expression.kind.unaryOperand.kind.tag == exprNumber {
			canonical := expression.kind.unaryOperand.kind.number.canonical
			if canonical != "0" {
				canonical = "-" + canonical
			}
			return numberLiteral(canonical), nil
		}
		return HclLiteralValue{}, NonLiteralExpressionError{}
	case exprParen:
		return LiteralValue(expression.kind.inner)
	}
	return HclLiteralValue{}, NonLiteralExpressionError{}
}

func literalKeyOf(key HclObjectKey) (HclLiteralKey, error) {
	switch key.tag {
	case keyIdentifier:
		return HclLiteralKey{tag: literalKeyIdentifier, text: key.name}, nil
	case keyNumber:
		return HclLiteralKey{tag: literalKeyNumber, text: key.number.canonical}, nil
	case keyTemplate:
		var text strings.Builder
		for i := range key.template.parts {
			part := &key.template.parts[i]
			switch part.tag {
			case partLiteral:
				text.WriteString(part.text)
			case partInterpolation, partDirective:
				return HclLiteralKey{}, NonLiteralExpressionError{}
			}
		}
		return HclLiteralKey{tag: literalKeyString, text: text.String()}, nil
	case keyParen:
		value, err := LiteralValue(key.parenInner)
		if err != nil {
			return HclLiteralKey{}, err
		}
		return HclLiteralKey{tag: literalKeyValue, value: value}, nil
	}
	return HclLiteralKey{}, NonLiteralExpressionError{}
}

func numberLiteral(canonical string) HclLiteralValue {
	if strings.Contains(canonical, ".") {
		return HclLiteralValue{tag: literalDecimal, text: canonical}
	}
	return HclLiteralValue{tag: literalInteger, text: canonical}
}

// stripHeredocIndentation applies the `<<-` indentation stripping: removes
// the minimum number of leading spaces from each line's leading literal
// text (RFC 0014 §4.5). The stripping is performed only when the
// template's literal value is read, never destructively.
func stripHeredocIndentation(text string) string {
	lines := strings.Split(text, "\n")
	minimum := -1
	for _, line := range lines {
		if line == "" {
			continue
		}
		indent := 0
		for indent < len(line) && line[indent] == ' ' {
			indent++
		}
		if minimum < 0 || indent < minimum {
			minimum = indent
		}
	}
	if minimum <= 0 {
		return text
	}
	var out strings.Builder
	for index, line := range lines {
		if index > 0 {
			out.WriteByte('\n')
		}
		if minimum < len(line) {
			out.WriteString(line[minimum:])
		}
	}
	return out.String()
}

// literalKindTag is the closed typed literal tag set.
type literalKindTag uint8

// The closed literal tags.
const (
	literalInteger literalKindTag = iota
	literalDecimal
	literalString
	literalBoolean
	literalNull
	literalTuple
	literalObject
)

// HclLiteralValue is the typed literal projection of a literal-complete
// expression (RFC 0014 §8.2).
//
// Integers and decimals carry the exact canonical decimal spelling with an
// optional leading `-`; the `hcl.body@1` projection converts them to the
// core BigInteger/Decimal members at projection time. Strings carry exact
// decoded code points. Tuple and object projections preserve source order,
// and duplicate object keys remain ordered entries.
type HclLiteralValue struct {
	tag      literalKindTag
	text     string
	boolean  bool
	elements []HclLiteralValue
	entries  []HclLiteralObjectEntry
}

// HclLiteralValueInteger creates an integer value: canonical decimal
// without a fraction, optional leading `-`.
func HclLiteralValueInteger(text string) HclLiteralValue {
	return HclLiteralValue{tag: literalInteger, text: text}
}

// HclLiteralValueDecimal creates a real value: canonical decimal with a
// fraction, optional leading `-`.
func HclLiteralValueDecimal(text string) HclLiteralValue {
	return HclLiteralValue{tag: literalDecimal, text: text}
}

// HclLiteralValueString creates a string value with exact decoded code
// points, including the `<<-` indentation-stripped heredoc content.
func HclLiteralValueString(text string) HclLiteralValue {
	return HclLiteralValue{tag: literalString, text: text}
}

// HclLiteralValueBoolean creates a boolean value.
func HclLiteralValueBoolean(value bool) HclLiteralValue {
	return HclLiteralValue{tag: literalBoolean, boolean: value}
}

// HclLiteralValueNull creates a null value.
func HclLiteralValueNull() HclLiteralValue { return HclLiteralValue{tag: literalNull} }

// HclLiteralValueTuple creates an ordered tuple of literal values.
func HclLiteralValueTuple(elements []HclLiteralValue) HclLiteralValue {
	return HclLiteralValue{tag: literalTuple, elements: elements}
}

// HclLiteralValueObject creates ordered object entries; duplicate keys are
// preserved.
func HclLiteralValueObject(entries []HclLiteralObjectEntry) HclLiteralValue {
	return HclLiteralValue{tag: literalObject, entries: entries}
}

// IsInteger reports whether the value is an integer.
func (v *HclLiteralValue) IsInteger() bool { return v.tag == literalInteger }

// IsDecimal reports whether the value is a real.
func (v *HclLiteralValue) IsDecimal() bool { return v.tag == literalDecimal }

// IsString reports whether the value is a string.
func (v *HclLiteralValue) IsString() bool { return v.tag == literalString }

// IsBoolean reports whether the value is a boolean.
func (v *HclLiteralValue) IsBoolean() bool { return v.tag == literalBoolean }

// IsNull reports whether the value is null.
func (v *HclLiteralValue) IsNull() bool { return v.tag == literalNull }

// IsTuple reports whether the value is a tuple.
func (v *HclLiteralValue) IsTuple() bool { return v.tag == literalTuple }

// IsObject reports whether the value is an object.
func (v *HclLiteralValue) IsObject() bool { return v.tag == literalObject }

// Text returns the canonical decimal (integer/real) or exact string text.
func (v *HclLiteralValue) Text() string { return v.text }

// Boolean returns the boolean value of a boolean literal.
func (v *HclLiteralValue) Boolean() bool { return v.boolean }

// Elements returns the ordered tuple elements. The returned slice must
// not be modified.
func (v *HclLiteralValue) Elements() []HclLiteralValue { return v.elements }

// Entries returns the ordered object entries. The returned slice must not
// be modified.
func (v *HclLiteralValue) Entries() []HclLiteralObjectEntry { return v.entries }

// HclLiteralObjectEntry is one ordered object literal entry in a
// HclLiteralValue object.
type HclLiteralObjectEntry struct {
	key   HclLiteralKey
	value HclLiteralValue
}

// NewHclLiteralObjectEntry creates one entry.
func NewHclLiteralObjectEntry(key HclLiteralKey, value HclLiteralValue) HclLiteralObjectEntry {
	return HclLiteralObjectEntry{key: key, value: value}
}

// Key returns the literal key.
func (e *HclLiteralObjectEntry) Key() *HclLiteralKey { return &e.key }

// Value returns the literal value.
func (e *HclLiteralObjectEntry) Value() *HclLiteralValue { return &e.value }

// literalKeyTag is the closed literal-key tag set.
type literalKeyTag uint8

// The closed literal key tags.
const (
	literalKeyIdentifier literalKeyTag = iota
	literalKeyNumber
	literalKeyString
	literalKeyValue
)

// HclLiteralKey is one object-literal key (RFC 0014 §8.1-§8.2).
//
// The three bare forms are an identifier, a number literal, and a quoted
// literal template; a parenthesized literal-complete expression reduces to
// its inner value.
type HclLiteralKey struct {
	tag   literalKeyTag
	text  string
	value HclLiteralValue
}

// HclLiteralKeyIdentifier creates a bare identifier key.
func HclLiteralKeyIdentifier(text string) HclLiteralKey {
	return HclLiteralKey{tag: literalKeyIdentifier, text: text}
}

// HclLiteralKeyNumber creates a bare number-literal key with the exact
// canonical decimal spelling.
func HclLiteralKeyNumber(text string) HclLiteralKey {
	return HclLiteralKey{tag: literalKeyNumber, text: text}
}

// HclLiteralKeyString creates a bare quoted-literal-template key with
// exact decoded text.
func HclLiteralKeyString(text string) HclLiteralKey {
	return HclLiteralKey{tag: literalKeyString, text: text}
}

// HclLiteralKeyValue creates a parenthesized literal-complete expression
// key.
func HclLiteralKeyValue(value HclLiteralValue) HclLiteralKey {
	return HclLiteralKey{tag: literalKeyValue, value: value}
}

// IsIdentifier reports whether the key is a bare identifier.
func (k *HclLiteralKey) IsIdentifier() bool { return k.tag == literalKeyIdentifier }

// IsNumber reports whether the key is a bare number literal.
func (k *HclLiteralKey) IsNumber() bool { return k.tag == literalKeyNumber }

// IsString reports whether the key is a bare quoted-literal-template key.
func (k *HclLiteralKey) IsString() bool { return k.tag == literalKeyString }

// IsValue reports whether the key is a parenthesized expression key.
func (k *HclLiteralKey) IsValue() bool { return k.tag == literalKeyValue }

// Text returns the exact key spelling (identifier, canonical decimal, or
// decoded string text).
func (k *HclLiteralKey) Text() string { return k.text }

// Value returns the reduced literal value of a parenthesized key.
func (k *HclLiteralKey) Value() *HclLiteralValue { return &k.value }
