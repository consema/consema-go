package main

// `consema query`: the query command (RFC 0015 §6.1). The request is the
// strict `cli.request@1` wrapper of flow.go with payload
// `core.query-definition@1`. Flow: parse the source under the request
// profile → bind the request's QueryDefinition (protocol package) →
// execute on the default portable projection of the document → emit the
// `core.query-result@1` record of RFC 0015 §6.1 with matches in source
// order. Cardinality failures (fail-not-null semantics, RequireOne) surface
// as data errors with the failed completion carrying
// `core.query.cardinality-violation@1`.
//
// The only wired domain is the portable-value domain; native domains need
// caller-externalized node locators, which the facade does not expose.
// XML/plist/HCL sources are rejected because their default projection
// publishes a versioned internal record that the portable domain cannot
// query.

import (
	"fmt"
	"io"

	"consema.dev/consema/core"
	"consema.dev/consema/protocol"
)

// runQuery runs `consema query` (request from --request-file or stdin).
func runQuery(parsed *ParsedArgs, stdout, stderr io.Writer) uint8 {
	request, err := readRequestBytes(parsed)
	if err != nil {
		return emitFailure(protocol.CommandQuery, parsed, err, nil, stdout, stderr)
	}
	return runQueryWithRequest(parsed, request, stdout, stderr)
}

// runQueryWithRequest runs `consema query` against already-read request
// bytes (testable without stdin or fixture files).
func runQueryWithRequest(parsed *ParsedArgs, request []byte,
	stdout, stderr io.Writer) uint8 {
	// The presentation redaction policy of RFC 0015 §11 (an invalid
	// `--redact-keys` pattern is a usage failure, like plan/apply/edit).
	policy, policyErr := compileRedactPolicy(parsed)
	if policyErr != nil {
		return emitFailure(protocol.CommandQuery, parsed, policyErr, nil, stdout, stderr)
	}
	input, err := decodeRequest(request, parsed, "core.query-definition@1")
	if err != nil {
		return emitFailure(protocol.CommandQuery, parsed, err, policy, stdout, stderr)
	}
	result, err := executeQuery(input)
	if err != nil {
		return emitFailure(protocol.CommandQuery, parsed, err, policy, stdout, stderr)
	}
	if parsed.json {
		resultValue, valueErr := result.ToValue()
		if valueErr != nil {
			return internalFailure("query", valueErr.Error(), stderr)
		}
		if emitErr := emitCommandEnvelope(protocol.CommandQuery, protocol.ExitSuccess,
			resultValue, nil, parsed, policy, stdout); emitErr != nil {
			return internalFailure("query", emitErr.Error(), stderr)
		}
		return protocol.ExitSuccess.ExitCode()
	}
	if writeErr := writeQueryReport(result, policy, stdout); writeErr != nil {
		return internalFailure("query", writeErr.Error(), stderr)
	}
	return protocol.ExitSuccess.ExitCode()
}

// executeQuery executes the request's query definition against the
// document's default portable projection, returning the
// `core.query-result@1` message with matches in source order.
func executeQuery(input *RequestInput) (*protocol.QueryResultMessage, *FlowError) {
	definition, failure := (&protocol.QueryDefinition{}).FromProtocolValue(input.Payload)
	if failure != nil {
		return nil, queryFailure(failure, "the query definition is invalid")
	}
	domain := definition.Domain()
	if domain.ID() != portableQueryDomain || domain.Version() != 1 {
		return nil, queryFailure(protocol.QueryFailureDomainMismatch(domain),
			fmt.Sprintf("query domain '%s@%d' is not wired in this milestone; only "+
				"%s@1 is supported (native domains need caller-externalized node "+
				"locators, which the facade does not yet expose)",
				domain.ID(), domain.Version(), portableQueryDomain))
	}
	document, err := parseDocument(input.Source, &input.Profile)
	if err != nil {
		return nil, err
	}
	if err := requireComplete(document, input.SourceLabel); err != nil {
		return nil, err
	}
	family := formatFamily(input.Profile.ID())
	if family == "" {
		return nil, newFlowError("cli.data.invalid-request@1",
			"profile '"+input.Profile.ID()+"' has no format family")
	}
	if family == "xml" || family == "plist" || family == "hcl" {
		return nil, newFlowError("cli.data.invalid-request@1",
			fmt.Sprintf("the %s@1 domain cannot query %s sources: their default "+
				"projection publishes a versioned internal record (the native query "+
				"domains require caller locators not yet exposed by the facade)",
				portableQueryDomain, family))
	}
	value, err := projectValue(document, mustDefaultProjection(family))
	if err != nil {
		return nil, err
	}
	validated, failure := definition.Validate()
	if failure != nil {
		return nil, queryFailure(failure, "the query definition failed validation")
	}
	role := validated.OutputRole()
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	executable, failure := validated.Bind(capabilities)
	if failure != nil {
		return nil, queryFailure(failure, "the query definition could not bind capabilities")
	}
	matches, failure := executable.ExecutePortable(value, protocol.DefaultQueryLimits())
	if failure != nil {
		return nil, queryFailure(failure, "the query execution failed")
	}
	wireMatches := make([]protocol.ProtocolQueryMatch, 0, len(matches))
	for _, match := range matches {
		wireMatches = append(wireMatches, protocol.ProtocolQueryMatch{
			Kind:  "Value",
			Path:  match.Path,
			Value: match.Value,
		})
	}
	result, resultErr := protocol.NewQueryResultFromPortableExecution(domain, role, wireMatches)
	if resultErr != nil {
		return nil, newFlowError("core.protocol.invalid-value@1",
			"query result encoding failed: "+resultErr.Error())
	}
	return result, nil
}

// mustDefaultProjection returns the default projection request of one
// family (the family gate above already validated it).
func mustDefaultProjection(family string) WireProjectionRequest {
	request, _ := defaultProjectionRequest(family)
	return request
}

// queryFailure maps one query failure to a data/limit-class failure
// carrying the failed `core.query-result@1` record (completion Failed with
// the stable code).
func queryFailure(failure *protocol.QueryFailure, message string) *FlowError {
	code := failure.Code()
	payload := failedQueryResult(code)
	flowError := newFlowError(code, message+" ("+code+")")
	if payload != nil {
		flowError = flowError.withPayload(payload)
	}
	return flowError
}

// failedQueryResult builds the failed `core.query-result@1` record form:
// zero matches, completion Failed with the stable code.
func failedQueryResult(code string) core.Value {
	completion, err := protocol.NewCompletionWithRegistry(protocol.CompletionFailed,
		0, 0, nil, &code, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		return nil
	}
	result, err := protocol.NewQueryResultMessage(protocol.DomainPortableValueV1(),
		protocol.RoleValue, nil, completion, nil)
	if err != nil {
		return nil
	}
	value, err := result.ToValue()
	if err != nil {
		return nil
	}
	return value
}

// writeQueryReport writes the deterministic human query report (same facade
// result as the machine payload).
//
// Presentation redaction (RFC 0015 §11.1): the value of every match whose
// key name matches the policy is replaced by the placeholder via redactText;
// `--show-secrets` disables matching. For value matches the candidate key is
// the last object-key path segment (a value matched through `$.password`
// redacts under `password`); matches without a key name (sequence elements,
// native locators) are never redacted.
func writeQueryReport(result *protocol.QueryResultMessage, policy *redactPolicy,
	stdout io.Writer) error {
	matches := result.Matches()
	if len(matches) == 0 {
		_, err := io.WriteString(stdout, "no matches\n")
		return err
	}
	for index, item := range matches {
		var line string
		switch item.Kind {
		case "Value":
			rendered := renderValue(item.Value)
			if key := lastObjectKey(item.Path); key != "" {
				rendered = redactText(policy, key, rendered).text
			}
			line = fmt.Sprintf("match %d: %s = %s", index,
				renderPath(item.Path), rendered)
		case "ObjectEntry":
			rendered := redactText(policy, renderKey(item.Key), renderValue(item.Value)).text
			line = fmt.Sprintf("match %d: %s (key %s) = %s", index,
				renderPath(item.ValuePath), renderKey(item.Key), rendered)
		case "EntryMappingEntry":
			rendered := renderValue(item.Value)
			if key, ok := item.Key.(core.String); ok {
				rendered = redactText(policy, string(key), rendered).text
			}
			line = fmt.Sprintf("match %d: %s (key %v) = %s", index,
				renderPath(item.ValuePath), renderValue(item.Key), rendered)
		case "Native":
			line = fmt.Sprintf("match %d: native %s %s", index,
				item.Native.NodeLocator(), item.Native.Role())
		default:
			line = fmt.Sprintf("match %d: %v", index, item)
		}
		if _, err := fmt.Fprintf(stdout, "%s\n", line); err != nil {
			return err
		}
	}
	return nil
}

// lastObjectKey returns the candidate redaction key of one value match: the
// last object-key path segment (the key whose value the match carries), or
// "" when the path ends in a sequence or entry-mapping segment.
func lastObjectKey(path protocol.ValuePath) string {
	segments := path.Segments()
	if len(segments) == 0 {
		return ""
	}
	last := segments[len(segments)-1]
	if last.Kind == "ObjectValue" {
		return last.Key
	}
	return ""
}

func renderKey(key core.Value) string {
	if text, ok := key.(core.String); ok {
		return string(text)
	}
	return renderValue(key)
}
