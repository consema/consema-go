package xml

// This file implements namespace-aware expanded names and the immutable
// binding scope (RFC 0012 §5; consema-rs/crates/consema-xml/src/namespace.rs). Prefix
// spelling is source representation. Expanded-name equality compares the
// namespace URI and the local name, never the prefix. Resolution follows
// Namespaces in XML 1.0 Third Edition without URI fetch or normalization.

// XMLNamespaceURI is the standard URI permanently bound to the `xml`
// prefix.
const XMLNamespaceURI = "http://www.w3.org/XML/1998/namespace"

// XMLNSNamespaceURI is the URI of the reserved `xmlns` prefix.
const XMLNSNamespaceURI = "http://www.w3.org/2000/xmlns/"

// QName is one lexical QName with its source-derived parts
// (namespace.rs:15-39).
type QName struct {
	// Prefix is the spelling before the colon, when present.
	Prefix *string
	// Local is the name after the colon, or the whole name when unprefixed.
	Local string
}

// NewQName creates a QName from an already split prefix and local name.
func NewQName(prefix *string, local string) QName {
	return QName{Prefix: prefix, Local: local}
}

// String returns the full lexical spelling `prefix:local` or `local`.
func (q QName) String() string {
	if q.Prefix == nil {
		return q.Local
	}
	return *q.Prefix + ":" + q.Local
}

// Equal reports whether two QNames are lexically identical.
func (q QName) Equal(other QName) bool {
	if q.Local != other.Local {
		return false
	}
	if q.Prefix == nil || other.Prefix == nil {
		return q.Prefix == other.Prefix
	}
	return *q.Prefix == *other.Prefix
}

// ExpandedName is a resolved expanded name = `{ namespace URI or none,
// local name }` (namespace.rs:41-57).
type ExpandedName struct {
	// Namespace is the namespace URI, or nil for an unprefixed attribute or
	// an unbound default namespace.
	Namespace *string
	// Local is the local name.
	Local string
}

// NewExpandedName creates an expanded name.
func NewExpandedName(namespace *string, local string) ExpandedName {
	return ExpandedName{Namespace: namespace, Local: local}
}

// Equal reports whether two expanded names are identical (prefix spelling
// never participates).
func (e ExpandedName) Equal(other ExpandedName) bool {
	if e.Local != other.Local {
		return false
	}
	if e.Namespace == nil || other.Namespace == nil {
		return e.Namespace == other.Namespace
	}
	return *e.Namespace == *other.Namespace
}

// Binding is one in-scope namespace binding (namespace.rs:59-66).
type Binding struct {
	// Prefix is the bound prefix; nil is the default namespace.
	Prefix *string
	// URI is the namespace URI.
	URI string
}

// NamespaceErrorKind classifies a namespace resolution failure
// (namespace.rs:68-89).
type NamespaceErrorKind uint8

// The closed namespace failure classes.
const (
	// NamespaceErrorUnboundPrefix: a prefixed name has no in-scope binding.
	NamespaceErrorUnboundPrefix NamespaceErrorKind = iota
	// NamespaceErrorReservedPrefix: `xmlns` or another reserved prefix was
	// used as an ordinary name or a declaration prefix.
	NamespaceErrorReservedPrefix
	// NamespaceErrorIllegalXmlRebinding: the `xml` prefix was declared to a
	// non-standard URI.
	NamespaceErrorIllegalXmlRebinding
	// NamespaceErrorIllegalDefaultXmlns: the `xmlns` URI was bound as the
	// default namespace.
	NamespaceErrorIllegalDefaultXmlns
)

// NamespaceError is one namespace resolution failure.
type NamespaceError struct {
	// Kind identifies the failure.
	Kind NamespaceErrorKind
	// Prefix is the offending prefix spelling.
	Prefix string
	// URI is the rejected URI of IllegalXmlRebinding.
	URI string
}

// Error implements error; the text is human presentation only.
func (e *NamespaceError) Error() string {
	switch e.Kind {
	case NamespaceErrorUnboundPrefix:
		return "xml: unbound prefix " + e.Prefix
	case NamespaceErrorReservedPrefix:
		return "xml: reserved prefix " + e.Prefix
	case NamespaceErrorIllegalXmlRebinding:
		return "xml: illegal xml prefix rebinding to " + e.URI
	case NamespaceErrorIllegalDefaultXmlns:
		return "xml: illegal default xmlns namespace"
	}
	return "xml: namespace error"
}

// Code returns the stable family code of the failure (RFC 0016 §6;
// parser.rs:130-137).
func (e *NamespaceError) Code() string { return namespaceCode(e) }

// NamespaceScope is an immutable, ancestry-derived namespace scope
// (namespace.rs:91-99). A scope is never mutated in place; declaring a
// binding appends to a new child scope, so the immutable ancestry chain of
// a tree is preserved.
type NamespaceScope struct {
	bindings []Binding
}

// NewNamespaceScope creates an empty scope holding only the permanent
// `xml` binding rule.
func NewNamespaceScope() NamespaceScope {
	return NamespaceScope{}
}

// Bindings returns all in-scope bindings in declaration order; a nil
// prefix is the default namespace. The returned slice is a copy.
func (s NamespaceScope) Bindings() []Binding {
	return append([]Binding(nil), s.bindings...)
}

// Declare appends one namespace declaration and returns the child scope.
// The `xmlns` prefix can never be declared, the `xml` prefix can only be
// declared to its standard URI, and the `xmlns` URI cannot become the
// default namespace (namespace.rs:117-144).
func (s NamespaceScope) Declare(prefix *string, uri string) (NamespaceScope, *NamespaceError) {
	if uri == XMLNSNamespaceURI && prefix == nil {
		return NamespaceScope{}, &NamespaceError{Kind: NamespaceErrorIllegalDefaultXmlns}
	}
	if prefix != nil {
		if *prefix == "xmlns" {
			return NamespaceScope{}, &NamespaceError{Kind: NamespaceErrorReservedPrefix, Prefix: *prefix}
		}
		if *prefix == "xml" && uri != XMLNamespaceURI {
			return NamespaceScope{}, &NamespaceError{Kind: NamespaceErrorIllegalXmlRebinding, Prefix: *prefix, URI: uri}
		}
	}
	bindings := make([]Binding, 0, len(s.bindings)+1)
	bindings = append(bindings, s.bindings...)
	bindings = append(bindings, Binding{Prefix: prefix, URI: uri})
	return NamespaceScope{bindings: bindings}, nil
}

// ResolveElement resolves an element name: the default namespace applies
// (namespace.rs:146-155).
func (s NamespaceScope) ResolveElement(qname QName) (ExpandedName, *NamespaceError) {
	if qname.Prefix == nil {
		return ExpandedName{Namespace: s.lookupDefault(), Local: qname.Local}, nil
	}
	return s.resolvePrefixed(qname)
}

// ResolveAttribute resolves an attribute name: the default namespace never
// applies (namespace.rs:157-166).
func (s NamespaceScope) ResolveAttribute(qname QName) (ExpandedName, *NamespaceError) {
	if qname.Prefix == nil {
		return ExpandedName{Namespace: nil, Local: qname.Local}, nil
	}
	return s.resolvePrefixed(qname)
}

// DeclarationExpandedName is the expanded name of a namespace declaration
// attribute itself: `xmlns` is `{ xmlns-URI, "xmlns" }` and `xmlns:p` is
// `{ xmlns-URI, "p" }`, used for attribute-uniqueness checks
// (namespace.rs:168-179).
func DeclarationExpandedName(prefix *string) ExpandedName {
	local := "xmlns"
	if prefix != nil {
		local = *prefix
	}
	return ExpandedName{Namespace: stringPtr(XMLNSNamespaceURI), Local: local}
}

func (s NamespaceScope) lookupDefault() *string {
	for index := len(s.bindings) - 1; index >= 0; index-- {
		if s.bindings[index].Prefix == nil {
			return &s.bindings[index].URI
		}
	}
	return nil
}

func (s NamespaceScope) resolvePrefixed(qname QName) (ExpandedName, *NamespaceError) {
	prefix := *qname.Prefix
	if prefix == "xml" {
		return ExpandedName{Namespace: stringPtr(XMLNamespaceURI), Local: qname.Local}, nil
	}
	if prefix == "xmlns" {
		return ExpandedName{}, &NamespaceError{Kind: NamespaceErrorReservedPrefix, Prefix: prefix}
	}
	for index := len(s.bindings) - 1; index >= 0; index-- {
		binding := s.bindings[index]
		if binding.Prefix != nil && *binding.Prefix == prefix {
			return ExpandedName{Namespace: &binding.URI, Local: qname.Local}, nil
		}
	}
	return ExpandedName{}, &NamespaceError{Kind: NamespaceErrorUnboundPrefix, Prefix: prefix}
}

func stringPtr(text string) *string { return &text }
