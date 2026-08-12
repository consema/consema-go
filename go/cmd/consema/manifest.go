package main

// Batch-manifest encoding and persistence — both sides of the manifest
// state machine (RFC 0015 §8.3 plan side / §9 result side; mirror of the
// Rust bin's manifest.rs). This module owns only the schema encoding, the
// strict decode, and the byte persistence of `core.batch-plan@1` and
// `core.batch-result@1`; the planning and applying semantics themselves are
// delegated to the SDK (the root package's BatchPlanner/ApplyPlanFile).
//
// The manifest record is encoded once through the canonical tagged JSON
// transport; the same bytes are carried by the `--json` envelope payload
// line and, with `--output`, persisted to that path via the fsio atomic
// engine (RFC 0015 §8.3: the file carries the same record without envelope
// wrapping, byte-identical to the envelope payload; the on-disk manifest is
// never redacted — RFC 0015 §8.3/§11.4, hard gate 3).
//
// Limits (RFC 0015 §12): the manifest size cap travels through the
// transport ProtocolLimits and surfaces as cli.limit.manifest-size@1 (limit
// class, exit 3). A manifest write failure keeps the fsio cli.write.* codes
// (precondition class, exit 4). A plan manifest that fails strict decode is
// a data error (cli.data.invalid-request@1, exit 2), except transport
// ResourceLimit (limit class).

import (
	"fmt"

	"consema.dev/consema/core"
	"consema.dev/consema/protocol"
)

// encodeManifest encodes one core.batch-plan@1 manifest as the canonical
// tagged JSON bytes (RFC 0015 §3.1). The same bytes are the envelope
// payload content and the --output file content.
func encodeManifest(manifest *protocol.BatchPlanMessage) ([]byte, *FlowError) {
	bytes, err := manifest.ToJSON(protocol.DefaultProtocolLimits())
	if err != nil {
		return nil, manifestSizeFlowError("plan manifest", err)
	}
	return bytes, nil
}

// persistManifest persists one plan manifest to --output through the fsio
// atomic engine (RFC 0015 §10; a write failure keeps the cli.write.* codes,
// precondition class, exit 4).
func persistManifest(path string, bytes []byte) *FlowError {
	_, err := writeAtomic(path, bytes, defaultWriteOptions())
	if err != nil {
		return newFlowError(err.Code, fmt.Sprintf("cannot write plan manifest '%s': %s",
			path, err.Message))
	}
	return nil
}

// decodePlanManifest strictly decodes one apply-input plan manifest
// (core.batch-plan@1, RFC 0015 §3.2/§8.3): the transport is chosen by
// magic (`PVCE` prefix -> PVCE/1, otherwise strict canonical JSON), and the
// record is revalidated through its typed decoder.
func decodePlanManifest(bytes []byte) (*protocol.BatchPlanMessage, *FlowError) {
	var value core.Value
	var err error
	if len(bytes) >= 4 && string(bytes[:4]) == "PVCE" {
		value, err = protocol.DecodePVCE(bytes, protocol.DefaultProtocolLimits())
	} else {
		value, err = protocol.DecodeJSON(bytes, protocol.DefaultProtocolLimits())
	}
	if err != nil {
		return nil, manifestDecodeFlowError(err)
	}
	plan := &protocol.BatchPlanMessage{}
	decoded, err := plan.FromValue(value)
	if err != nil {
		return nil, newFlowError("cli.data.invalid-request@1",
			"plan manifest is not a byte-valid core.batch-plan@1 record: "+err.Error())
	}
	return decoded, nil
}

// encodeResultManifest encodes one core.batch-result@1 manifest as the
// canonical tagged JSON bytes (RFC 0015 §3.1).
func encodeResultManifest(manifest *protocol.BatchResultMessage) ([]byte, *FlowError) {
	bytes, err := manifest.ToJSON(protocol.DefaultProtocolLimits())
	if err != nil {
		return nil, manifestSizeFlowError("result manifest", err)
	}
	return bytes, nil
}

// persistResultManifest persists one result manifest (pending, completed,
// failed, or interrupted state; RFC 0015 §9.3) through the fsio atomic
// engine. A write failure keeps the cli.write.* codes, precondition class,
// exit 4.
func persistResultManifest(path string, bytes []byte) *FlowError {
	_, err := writeAtomic(path, bytes, defaultWriteOptions())
	if err != nil {
		return newFlowError(err.Code, fmt.Sprintf("cannot write result manifest '%s': %s",
			path, err.Message))
	}
	return nil
}

// manifestSizeFlowError maps a transport ResourceLimit onto the frozen
// cli.limit.manifest-size@1 failure (RFC 0015 §12).
func manifestSizeFlowError(kind string, err error) *FlowError {
	if protocolError, ok := err.(*protocol.ProtocolError); ok &&
		protocolError.Kind == protocol.KindResourceLimit {
		return newFlowError("cli.limit.manifest-size@1",
			kind+" exceeds the transport byte cap: "+err.Error())
	}
	return protocolFlowError(err)
}

// manifestDecodeFlowError maps a manifest transport failure per RFC 0015
// §3.2: decode ResourceLimit is a limit error (cli.limit.manifest-size@1),
// everything else is a data error (cli.data.invalid-request@1).
func manifestDecodeFlowError(err error) *FlowError {
	if protocolError, ok := err.(*protocol.ProtocolError); ok &&
		protocolError.Kind == protocol.KindResourceLimit {
		return newFlowError("cli.limit.manifest-size@1",
			"plan manifest exceeds the transport byte cap: "+err.Error())
	}
	return protocolFlowError(err)
}
