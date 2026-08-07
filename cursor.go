package consema

// This file implements the ordered query cursor terminal semantics
// (crates/consema-core/src/query.rs `OrderedQueryCursor` /
// `QueryTerminalState`; capability `core.query.cursor-terminal@1`). The
// cursor is the language-neutral pull primitive behind the query streams:
// every value is yielded in standard order, and the stream terminates with
// exactly one closed terminal state — Completed when every value was
// yielded, Cancelled when cancellation pre-empted the stream, Failed when
// the stream was declared failing. While the stream is open the terminal
// state is not yet established.
//
// The query engine of go/protocol owns the complete-execution surface; this
// root package hosts the cursor primitive so the conformance faces can
// exercise the terminal semantics without a protocol package change (the
// go/protocol cursor API lands with the 0.19.0 Go CLI work).

import (
	"context"

	"consema.dev/consema/core"
)

// CursorTerminalState is one closed terminal state of an ordered cursor
// stream (query.rs QueryTerminalState).
type CursorTerminalState string

// The three frozen terminal states.
const (
	// CursorCompleted: every value of the stream was yielded.
	CursorCompleted CursorTerminalState = "Completed"
	// CursorCancelled: cancellation stopped the stream before exhaustion.
	CursorCancelled CursorTerminalState = "Cancelled"
	// CursorFailed: the stream failed after zero or more yielded values.
	CursorFailed CursorTerminalState = "Failed"
)

// OrderedCursor is the pull cursor over an already ordered standard result
// (query.rs:3049-3104). `Next` yields values in order; the terminal state
// is hidden until the stream closes: exhaustion closes with the declared
// terminal, and a cancellation cursor closes with Cancelled as soon as the
// context is cancelled.
type OrderedCursor struct {
	values    []core.Value
	index     int
	declared  CursorTerminalState
	terminal  *CursorTerminalState
	cancelCtx context.Context
}

// NewOrderedCursor creates a cursor that completes after every value is
// yielded (query.rs OrderedQueryCursor::new).
func NewOrderedCursor(values []core.Value) *OrderedCursor {
	return NewOrderedCursorWithTerminal(values, CursorCompleted)
}

// NewOrderedCursorWithTerminal creates a cursor with an explicit terminal
// state that remains hidden until exhaustion (query.rs
// OrderedQueryCursor::with_terminal).
func NewOrderedCursorWithTerminal(values []core.Value,
	terminal CursorTerminalState) *OrderedCursor {
	return &OrderedCursor{
		values:   append([]core.Value(nil), values...),
		declared: terminal,
	}
}

// NewOrderedCursorWithCancellation creates a cursor that stops with
// Cancelled as soon as the context is cancelled (query.rs
// OrderedQueryCursor::with_cancellation; the Go ecosystem cancellation
// signal is context.Context).
func NewOrderedCursorWithCancellation(values []core.Value,
	ctx context.Context) *OrderedCursor {
	return &OrderedCursor{
		values:    append([]core.Value(nil), values...),
		declared:  CursorCompleted,
		cancelCtx: ctx,
	}
}

// Next yields the next value; ok is false when the stream is closed. The
// terminal state is set exactly when the stream closes.
func (c *OrderedCursor) Next() (value core.Value, ok bool) {
	if c.terminal != nil {
		return core.NullValue(), false
	}
	if c.cancelCtx != nil && c.cancelCtx.Err() != nil {
		terminal := CursorCancelled
		c.terminal = &terminal
		return core.NullValue(), false
	}
	if c.index >= len(c.values) {
		terminal := c.declared
		c.terminal = &terminal
		return core.NullValue(), false
	}
	value = c.values[c.index]
	c.index++
	return value, true
}

// TerminalState returns the established terminal state, or nil while the
// stream is still open (query.rs OrderedQueryCursor::terminal_state).
func (c *OrderedCursor) TerminalState() *CursorTerminalState {
	return c.terminal
}
