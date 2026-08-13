package consema

// This file implements the lazy ordered pull cursor over one validated
// portable-value query (consema-rs/crates/consema-core/src/query.rs
// `PortableQueryCursor` and `build_portable_cursor_pipeline`; capability
// `core.query.ordered-results@1`). The cursor yields standard-order matches
// one at a time; mid-stream failures surface through NextMatch with a
// Failed terminal, cancellation surfaces Cancelled, and exhaustion closes
// with Completed. Matches yielded before a failure remain real local
// discoveries (RFC 0004 §19: no failure returns a partial result that can
// be mistaken for a complete one).
//
// The engine is built on the public query surface of go/protocol
// (ExecutableQuery.Definition().Expression() etc.) and mirrors the
// operator subset of protocol query execution (core.take,
// core.distinct-by-identity) plus core.try-sequence-elements. The
// go/protocol cursor API lands with the 0.19.0 Go CLI work; until then the
// root package hosts the engine so the shared vectors execute.

import (
	"context"
	"strconv"
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/protocol"
)

// PortableCursor is the lazy ordered pull cursor over one validated
// portable-value query. Definition and capability errors fail before the
// first match (NewPortableCursor); mid-stream failures surface through
// NextMatch.
type PortableCursor struct {
	root     cursorProducer
	ctx      context.Context
	limits   protocol.QueryLimits
	steps    int
	terminal *CursorTerminalState
}

// NewPortableCursor builds the cursor pipeline for one validated and
// capability-bound portable-value query (query.rs
// build_portable_cursor_pipeline). Unsupported expression kinds or
// operators fail before the first match.
func NewPortableCursor(executable *protocol.ExecutableQuery, value core.Value,
	limits protocol.QueryLimits, ctx context.Context) (*PortableCursor, error) {
	definition := executable.Definition()
	if !definition.Domain().Equal(protocol.DomainPortableValueV1()) {
		return nil, &protocol.QueryFailure{Kind: protocol.FailureDomainMismatch,
			Domain: definition.Domain()}
	}
	rootMatch := protocol.PortableMatch{Path: protocol.RootValuePath(), Value: value}
	producer, failure := buildCursorProducer(definition.Expression(), rootMatch)
	if failure != nil {
		return nil, failure
	}
	producer, failure = buildSelectionProducer(definition.Selection(), producer)
	if failure != nil {
		return nil, failure
	}
	return &PortableCursor{
		root:   &counterProducer{child: producer, maxResults: limits.MaxResults},
		ctx:    ctx,
		limits: limits,
	}, nil
}

// NextMatch pulls the next match (query.rs PortableQueryCursor::next_match):
// ok is true for one more stream item, which is either a yielded match
// (failure nil) or the terminal failure that stops the stream (failure
// non-nil; the terminal state is established). ok is false when the stream
// is closed.
func (c *PortableCursor) NextMatch() (protocol.PortableMatch, *protocol.QueryFailure, bool) {
	if c.terminal != nil {
		return protocol.PortableMatch{}, nil, false
	}
	if c.ctx != nil && c.ctx.Err() != nil {
		terminal := CursorCancelled
		c.terminal = &terminal
		return protocol.PortableMatch{},
			&protocol.QueryFailure{Kind: protocol.FailureCancelled}, true
	}
	c.steps++
	if c.steps > c.limits.MaxSteps {
		terminal := CursorFailed
		c.terminal = &terminal
		return protocol.PortableMatch{},
			&protocol.QueryFailure{Kind: protocol.FailureResourceLimit}, true
	}
	match, done, failure := c.root.next()
	if failure != nil {
		terminal := CursorFailed
		if failure.Kind == protocol.FailureCancelled {
			terminal = CursorCancelled
		}
		c.terminal = &terminal
		return protocol.PortableMatch{}, failure, true
	}
	if done {
		terminal := CursorCompleted
		c.terminal = &terminal
		return protocol.PortableMatch{}, nil, false
	}
	return match, nil, true
}

// TerminalState returns the established terminal state, or nil while the
// stream is still open (query.rs PortableQueryCursor::terminal_state).
func (c *PortableCursor) TerminalState() *CursorTerminalState {
	return c.terminal
}

// cursorProducer is one lazy pipeline stage.
type cursorProducer interface {
	// next yields the next match; done closes the stage; failure aborts
	// the stream.
	next() (protocol.PortableMatch, bool, *protocol.QueryFailure)
}

// inputProducer yields the domain root match once (query.rs
// InputProducer).
type inputProducer struct {
	match protocol.PortableMatch
	done  bool
}

func (p *inputProducer) next() (protocol.PortableMatch, bool, *protocol.QueryFailure) {
	if p.done {
		return protocol.PortableMatch{}, true, nil
	}
	p.done = true
	return p.match, false, nil
}

// applyProducer applies one operator to the child stream, buffering the
// operator's expansion of each child match (query.rs
// apply_portable_operator_items producer).
type applyProducer struct {
	operator *protocol.OperatorCall
	child    cursorProducer
	buffer   []protocol.PortableMatch
	index    int
	done     bool
	// take state.
	taken   int
	takeArg int
	// distinct state.
	seen map[string]bool
}

func newApplyProducer(operator *protocol.OperatorCall,
	child cursorProducer) (cursorProducer, *protocol.QueryFailure) {
	switch operator.ID() {
	case "core.take":
		return &applyProducer{operator: operator, child: child,
			takeArg: cursorTakeCount(operator)}, nil
	case "core.distinct-by-identity":
		return &applyProducer{operator: operator, child: child,
			seen: map[string]bool{}}, nil
	case "core.try-sequence-elements":
		return &applyProducer{operator: operator, child: child}, nil
	}
	return nil, &protocol.QueryFailure{Kind: protocol.FailureTargetUnavailable,
		Operator: operator.ID(), Version: operator.Version()}
}

func (p *applyProducer) next() (protocol.PortableMatch, bool, *protocol.QueryFailure) {
	for {
		if p.index < len(p.buffer) {
			match := p.buffer[p.index]
			p.index++
			return match, false, nil
		}
		if p.done {
			return protocol.PortableMatch{}, true, nil
		}
		closed, failure := p.refill()
		if failure != nil {
			return protocol.PortableMatch{}, false, failure
		}
		if closed && len(p.buffer) == 0 {
			return protocol.PortableMatch{}, true, nil
		}
	}
}

// refill pulls child matches until the buffer holds at least one output
// match or the child stream closes.
func (p *applyProducer) refill() (closed bool, failure *protocol.QueryFailure) {
	for {
		match, done, failure := p.child.next()
		if failure != nil {
			return false, failure
		}
		if done {
			p.done = true
			return true, nil
		}
		switch p.operator.ID() {
		case "core.take":
			if p.taken >= p.takeArg {
				p.done = true
				return true, nil
			}
			p.buffer = append(p.buffer, match)
			p.taken++
		case "core.distinct-by-identity":
			key := cursorMatchIdentity(match)
			if p.seen[key] {
				continue
			}
			p.seen[key] = true
			p.buffer = append(p.buffer, match)
		case "core.try-sequence-elements":
			array, ok := match.Value.(*core.Array)
			if !ok {
				continue
			}
			for index, element := range array.Items() {
				p.buffer = append(p.buffer, protocol.PortableMatch{
					Path: match.Path.Child(protocol.ValuePathSegment{
						Kind: "SequenceElement", Index: uint64(index),
					}),
					Value: element,
				})
			}
		}
		if len(p.buffer) > 0 {
			return false, nil
		}
	}
}

// concatProducer yields every branch's stream in branch order (query.rs
// concat composition).
type concatProducer struct {
	branches []cursorProducer
	index    int
}

func (p *concatProducer) next() (protocol.PortableMatch, bool, *protocol.QueryFailure) {
	for p.index < len(p.branches) {
		match, done, failure := p.branches[p.index].next()
		if failure != nil {
			return protocol.PortableMatch{}, false, failure
		}
		if !done {
			return match, false, nil
		}
		p.index++
	}
	return protocol.PortableMatch{}, true, nil
}

// selectionProducer applies the cardinality selection to the stream
// (query.rs selection_producer). All passes through; First closes after
// one match; Last/ZeroOrOne/RequireOne buffer the complete stream.
type selectionProducer struct {
	selection protocol.QuerySelection
	child     cursorProducer
	state     int // 0 = open, 1 = emitted, 2 = closed
	buffer    []protocol.PortableMatch
}

func buildSelectionProducer(selection protocol.QuerySelection,
	child cursorProducer) (cursorProducer, *protocol.QueryFailure) {
	return &selectionProducer{selection: selection, child: child}, nil
}

func (p *selectionProducer) next() (protocol.PortableMatch, bool, *protocol.QueryFailure) {
	for {
		if p.state == 2 {
			return protocol.PortableMatch{}, true, nil
		}
		switch p.selection {
		case protocol.SelectionAll, "":
			return p.child.next()
		case protocol.SelectionFirst:
			match, done, failure := p.child.next()
			if failure != nil {
				return protocol.PortableMatch{}, false, failure
			}
			if done {
				p.state = 2
				return protocol.PortableMatch{}, true, nil
			}
			p.state = 2
			return match, false, nil
		case protocol.SelectionLast, protocol.SelectionZeroOrOne, protocol.SelectionRequireOne:
			if p.state == 0 {
				limit := 2
				for {
					match, done, failure := p.child.next()
					if failure != nil {
						return protocol.PortableMatch{}, false, failure
					}
					if done {
						break
					}
					p.buffer = append(p.buffer, match)
					if p.selection != protocol.SelectionLast && len(p.buffer) >= limit {
						break
					}
				}
				p.state = 1
			}
			if p.state == 1 {
				switch p.selection {
				case protocol.SelectionLast:
					if len(p.buffer) == 0 {
						p.state = 2
						return protocol.PortableMatch{}, true, nil
					}
					match := p.buffer[len(p.buffer)-1]
					p.state = 2
					return match, false, nil
				case protocol.SelectionZeroOrOne:
					if len(p.buffer) > 1 {
						p.state = 2
						return protocol.PortableMatch{}, false,
							&protocol.QueryFailure{Kind: protocol.FailureCardinalityViolation}
					}
				case protocol.SelectionRequireOne:
					if len(p.buffer) != 1 {
						p.state = 2
						return protocol.PortableMatch{}, false,
							&protocol.QueryFailure{Kind: protocol.FailureCardinalityViolation}
					}
				}
				if len(p.buffer) == 0 {
					p.state = 2
					return protocol.PortableMatch{}, true, nil
				}
				match := p.buffer[0]
				p.state = 2
				return match, false, nil
			}
		}
		return protocol.PortableMatch{}, true, nil
	}
}

// counterProducer enforces the max_results budget over the yielded stream
// (query.rs RootCounter): the root check happens before the first match,
// and the count check after every yielded match.
type counterProducer struct {
	child       cursorProducer
	maxResults  int
	rootChecked bool
	count       int
}

func (p *counterProducer) next() (protocol.PortableMatch, bool, *protocol.QueryFailure) {
	if !p.rootChecked {
		p.rootChecked = true
		if 1 > p.maxResults {
			return protocol.PortableMatch{}, false,
				&protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
		}
	}
	match, done, failure := p.child.next()
	if failure != nil {
		return protocol.PortableMatch{}, false, failure
	}
	if done {
		return protocol.PortableMatch{}, true, nil
	}
	p.count++
	if p.count > p.maxResults {
		return protocol.PortableMatch{}, false,
			&protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	return match, false, nil
}

// buildCursorProducer builds the pipeline for one expression (query.rs
// build_producer).
func buildCursorProducer(expression *protocol.QueryExpression,
	rootMatch protocol.PortableMatch) (cursorProducer, *protocol.QueryFailure) {
	switch expression.Kind {
	case protocol.ExpressionInput:
		return &inputProducer{match: rootMatch}, nil
	case protocol.ExpressionApply:
		child, failure := buildCursorProducer(expression.Input, rootMatch)
		if failure != nil {
			return nil, failure
		}
		producer, failure := newApplyProducer(expression.Operator, child)
		if failure != nil {
			return nil, failure
		}
		return producer, nil
	case protocol.ExpressionConcat:
		branches := make([]cursorProducer, 0, len(expression.Branches))
		for _, branch := range expression.Branches {
			producer, failure := buildCursorProducer(branch, rootMatch)
			if failure != nil {
				return nil, failure
			}
			branches = append(branches, producer)
		}
		return &concatProducer{branches: branches}, nil
	case protocol.ExpressionStructureOrderMerge:
		return nil, &protocol.QueryFailure{Kind: protocol.FailureTargetUnavailable,
			Operator: "composition.structure-order-merge"}
	}
	return nil, &protocol.QueryFailure{Kind: protocol.FailureTargetUnavailable,
		Operator: "expression"}
}

// cursorMatchIdentity is the distinct-by-identity key of one portable
// match (the Rust PortableIdentity::Value(path); query.rs:2248-2262).
func cursorMatchIdentity(match protocol.PortableMatch) string {
	var key strings.Builder
	for _, segment := range match.Path.Segments() {
		key.WriteString(segment.Kind)
		key.WriteByte(':')
		content := segment.Key
		if segment.Kind != "ObjectValue" {
			content = strconv.FormatUint(segment.Index, 10)
		}
		key.WriteString(strconv.Itoa(len(content)))
		key.WriteByte(':')
		key.WriteString(content)
		key.WriteByte(';')
	}
	return key.String()
}

// cursorTakeCount reads the core.take count argument.
func cursorTakeCount(operator *protocol.OperatorCall) int {
	argument, ok := operator.Arguments()["count"]
	if !ok {
		return 0
	}
	integer, ok := argument.(core.Integer)
	if !ok {
		return 0
	}
	number := integer.Int()
	if number.Sign() < 0 || !number.IsUint64() || number.BitLen() > 62 {
		return 0
	}
	return int(number.Uint64())
}
