package protocol

// Completion, cancellation, and execution-policy wire records
// (consema-rs/crates/consema-protocol/src/execution.rs). These are the
// `core.completion@1`, `core.cancellation-request@1`, and
// `core.execution-policy@1` language-neutral records; cancellation tokens
// themselves stay process-local and are never serialized.

import (
	"sort"

	"consema.dev/consema/core"
)

// CompletionStatus is the closed language-neutral completion state
// (execution.rs:12-25).
type CompletionStatus string

// The six frozen completion statuses.
const (
	CompletionSuccess         CompletionStatus = "Success"
	CompletionFailed          CompletionStatus = "Failed"
	CompletionCancelled       CompletionStatus = "Cancelled"
	CompletionResourceLimited CompletionStatus = "ResourceLimited"
	CompletionUnsupported     CompletionStatus = "Unsupported"
	CompletionNotApplicable   CompletionStatus = "NotApplicable"
)

// String returns the canonical status spelling.
func (s CompletionStatus) String() string { return string(s) }

// Completion is the `core.completion@1` control-flow facts record
// (execution.rs:40-49).
type Completion struct {
	status      CompletionStatus
	processed   uint64
	produced    uint64
	limitName   *string
	failureCode *string
}

// NewCompletion validates the state-specific completion invariants against
// the semantic-model v1 error registry (execution.rs:51-67).
func NewCompletion(status CompletionStatus, processed, produced uint64,
	limitName, failureCode *string) (*Completion, error) {
	return NewCompletionWithRegistry(status, processed, produced, limitName, failureCode,
		DefaultErrorCodeRegistry())
}

// NewCompletionWithRegistry validates completion facts against one explicit
// semantic-model error registry (execution.rs:69-107): a failure code must
// be registered, Success/Cancelled carry no limit or failure facts,
// ResourceLimited requires a non-empty limit name, and
// Failed/Unsupported/NotApplicable require a non-empty registered failure
// code.
func NewCompletionWithRegistry(status CompletionStatus, processed, produced uint64,
	limitName, failureCode *string, registry ErrorCodeRegistry) (*Completion, error) {
	if failureCode != nil {
		if err := registry.validateAt(*failureCode, "$.failure_code"); err != nil {
			return nil, err
		}
	}
	valid := false
	switch status {
	case CompletionSuccess, CompletionCancelled:
		valid = limitName == nil && failureCode == nil
	case CompletionResourceLimited:
		valid = limitName != nil && *limitName != "" && failureCode == nil
	case CompletionFailed, CompletionUnsupported, CompletionNotApplicable:
		valid = limitName == nil && failureCode != nil && *failureCode != ""
	}
	if !valid {
		return nil, invalid("$", "completion status contradicts limit/failure fields")
	}
	return &Completion{
		status:      status,
		processed:   processed,
		produced:    produced,
		limitName:   limitName,
		failureCode: failureCode,
	}, nil
}

// Status returns the completion state.
func (c *Completion) Status() CompletionStatus { return c.status }

// Processed returns the work items consumed before the terminal state.
func (c *Completion) Processed() uint64 { return c.processed }

// Produced returns the complete or locally discovered output count.
func (c *Completion) Produced() uint64 { return c.produced }

// LimitName returns the limit that stopped execution, when limited.
func (c *Completion) LimitName() *string { return c.limitName }

// FailureCode returns the stable terminal failure code, when failed.
func (c *Completion) FailureCode() *string { return c.failureCode }

// ToValue encodes `core.completion@1` (execution.rs:140-153).
func (c *Completion) ToValue() (core.Value, error) {
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.completion@1")},
		core.Entry{Key: "status", Value: core.String(string(c.status))},
		core.Entry{Key: "processed", Value: integerValue(c.processed)},
		core.Entry{Key: "produced", Value: integerValue(c.produced)},
		core.Entry{Key: "limit_name", Value: nullableString(c.limitName)},
		core.Entry{Key: "failure_code", Value: nullableString(c.failureCode)},
	)
}

// FromValue strictly decodes `core.completion@1` under the v1 registry
// (execution.rs:155-158).
func (c *Completion) FromValue(value core.Value) (*Completion, error) {
	return c.FromValueWithRegistry(value, DefaultErrorCodeRegistry())
}

// FromValueWithRegistry strictly decodes `core.completion@1` under one
// explicit semantic-model registry (execution.rs:160-186).
func (c *Completion) FromValueWithRegistry(value core.Value, registry ErrorCodeRegistry) (*Completion, error) {
	fields, err := schemaFields(value, "core.completion@1",
		[]string{"schema", "status", "processed", "produced", "limit_name", "failure_code"}, "$")
	if err != nil {
		return nil, err
	}
	status, err := parseCompletionStatus(fields[1], "$.status")
	if err != nil {
		return nil, err
	}
	processed, err := unsigned64(fields[2], "$.processed")
	if err != nil {
		return nil, err
	}
	produced, err := unsigned64(fields[3], "$.produced")
	if err != nil {
		return nil, err
	}
	limitName, err := optionalString(fields[4], "$.limit_name")
	if err != nil {
		return nil, err
	}
	failureCode, err := optionalString(fields[5], "$.failure_code")
	if err != nil {
		return nil, err
	}
	return NewCompletionWithRegistry(status, processed, produced, limitName, failureCode, registry)
}

// ProcessLocalHandleError returns the frozen externalization failure for
// process-local handles: the Go records are wire-externalized by
// construction, so any attempted externalization of a raw process-local
// node handle is rejected with core.protocol.process-local-handle@1
// (diagnostic.rs:353-381; query.rs:92-97; projection.rs:66-74).
func ProcessLocalHandleError(path string) error {
	return protocolError(KindProcessLocalHandle, path,
		"process-local handle must be externalized to a stable caller identity")
}

// validLimitName reports whether a name is a stable lowercase identifier
// (execution.rs:368-374): 1-255 characters of lowercase ASCII letters,
// digits, underscores, and dashes.
func validLimitName(name string) bool {
	if name == "" || len(name) > 255 {
		return false
	}
	for index := 0; index < len(name); index++ {
		byte := name[index]
		if !(byte >= 'a' && byte <= 'z') && !(byte >= '0' && byte <= '9') &&
			byte != '_' && byte != '-' {
			return false
		}
	}
	return true
}

func parseCompletionStatus(value core.Value, path string) (CompletionStatus, error) {
	text, err := stringOf(value, path)
	if err != nil {
		return "", err
	}
	switch CompletionStatus(text) {
	case CompletionSuccess, CompletionFailed, CompletionCancelled,
		CompletionResourceLimited, CompletionUnsupported, CompletionNotApplicable:
		return CompletionStatus(text), nil
	}
	return "", invalid(path, "unknown completion status")
}

// ExecutionPolicy is the transferable `core.execution-policy@1` record;
// cancellation tokens remain process-local (execution.rs:189-195).
type ExecutionPolicy struct {
	limits                map[string]uint64
	cancellationRequestID *string
}

// NewExecutionPolicy creates a policy with deterministically sorted unique
// limit names (execution.rs:196-221).
func NewExecutionPolicy(limits map[string]uint64, cancellationRequestID *string) (*ExecutionPolicy, error) {
	for name := range limits {
		if !validLimitName(name) {
			return nil, invalid("$.limits", "limit names must be stable lowercase identifiers")
		}
	}
	if cancellationRequestID != nil &&
		(*cancellationRequestID == "" || len(*cancellationRequestID) > 1024) {
		return nil, invalid("$.cancellation_request_id", "invalid cancellation request ID")
	}
	return &ExecutionPolicy{limits: limits, cancellationRequestID: cancellationRequestID}, nil
}

// Limits returns the named limits sorted by key.
func (p *ExecutionPolicy) Limits() map[string]uint64 {
	output := make(map[string]uint64, len(p.limits))
	for name, value := range p.limits {
		output[name] = value
	}
	return output
}

// CancellationRequestID returns the optional outer-transport cancellation
// request ID.
func (p *ExecutionPolicy) CancellationRequestID() *string { return p.cancellationRequestID }

// ToValue encodes `core.execution-policy@1` (execution.rs:235-252).
func (p *ExecutionPolicy) ToValue() (core.Value, error) {
	names := make([]string, 0, len(p.limits))
	for name := range p.limits {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]core.Entry, 0, len(names)+1)
	entries = append(entries, core.Entry{Key: "schema", Value: core.String("core.execution-policy@1")})
	limitEntries := make([]core.Entry, 0, len(names))
	for _, name := range names {
		limitEntries = append(limitEntries, core.Entry{Key: name, Value: integerValue(p.limits[name])})
	}
	limits, err := core.NewObject(limitEntries...)
	if err != nil {
		return nil, err
	}
	entries = append(entries, core.Entry{Key: "limits", Value: limits})
	entries = append(entries, core.Entry{Key: "cancellation_request_id", Value: nullableString(p.cancellationRequestID)})
	return core.NewObject(entries...)
}

// FromValue strictly decodes `core.execution-policy@1`
// (execution.rs:254-277).
func (p *ExecutionPolicy) FromValue(value core.Value) (*ExecutionPolicy, error) {
	fields, err := schemaFields(value, "core.execution-policy@1",
		[]string{"schema", "limits", "cancellation_request_id"}, "$")
	if err != nil {
		return nil, err
	}
	object, ok := fields[1].(*core.Object)
	if !ok {
		return nil, protocolError(KindWrongType, "$.limits", "expected Object<String, Integer>")
	}
	limits := make(map[string]uint64, len(object.Entries()))
	for _, entry := range object.Entries() {
		number, err := unsigned64(entry.Value, "$.limits."+entry.Key)
		if err != nil {
			return nil, err
		}
		limits[entry.Key] = number
	}
	cancellationRequestID, err := optionalString(fields[2], "$.cancellation_request_id")
	if err != nil {
		return nil, err
	}
	return NewExecutionPolicy(limits, cancellationRequestID)
}

// CancellationRequest is the idempotent outer-transport
// `core.cancellation-request@1` record; it is not a serialized
// CancellationToken (execution.rs:279-290).
type CancellationRequest struct {
	requestID string
	reason    *string
}

// NewCancellationRequest creates a request with a bounded stable ID
// (execution.rs:291-302).
func NewCancellationRequest(requestID string, reason *string) (*CancellationRequest, error) {
	if requestID == "" || len(requestID) > 1024 {
		return nil, invalid("$.request_id", "invalid request ID")
	}
	return &CancellationRequest{requestID: requestID, reason: reason}, nil
}

// RequestID returns the transport request ID.
func (r *CancellationRequest) RequestID() string { return r.requestID }

// Reason returns the optional stable reason or operator note.
func (r *CancellationRequest) Reason() *string { return r.reason }

// ToValue encodes `core.cancellation-request@1` (execution.rs:312-325).
func (r *CancellationRequest) ToValue() (core.Value, error) {
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.cancellation-request@1")},
		core.Entry{Key: "request_id", Value: core.String(r.requestID)},
		core.Entry{Key: "reason", Value: nullableString(r.reason)},
	)
}

// FromValue strictly decodes `core.cancellation-request@1`
// (execution.rs:327-340).
func (r *CancellationRequest) FromValue(value core.Value) (*CancellationRequest, error) {
	fields, err := schemaFields(value, "core.cancellation-request@1",
		[]string{"schema", "request_id", "reason"}, "$")
	if err != nil {
		return nil, err
	}
	requestID, err := stringOf(fields[1], "$.request_id")
	if err != nil {
		return nil, err
	}
	reason, err := optionalString(fields[2], "$.reason")
	if err != nil {
		return nil, err
	}
	return NewCancellationRequest(requestID, reason)
}
