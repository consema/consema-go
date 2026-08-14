package document

// MaterializationRequest is the complete immutable request for creating
// one new target document (materialization.rs; RFC 0016 §5.2).
type MaterializationRequest struct {
	targetProfile    ProfileId
	style            MaterializationStyleId
	encoding         SourceEncoding
	newline          NewlinePolicy
	mappingPolicy    MappingPolicy
	representability RepresentabilityPolicy
	limits           MaterializationLimits
}

// NewMaterializationRequest creates a strict request with UTF-8, LF,
// Object-only, and ExactOnly defaults.
func NewMaterializationRequest(targetProfile ProfileId, style MaterializationStyleId) MaterializationRequest {
	return MaterializationRequest{
		targetProfile:    targetProfile,
		style:            style,
		encoding:         Utf8Encoding(),
		newline:          NewlineLf,
		mappingPolicy:    MappingPolicyRequireObject,
		representability: RepresentabilityPolicyExactOnly,
		limits:           DefaultMaterializationLimits(),
	}
}

// WithEncoding selects an explicit output encoding.
func (r MaterializationRequest) WithEncoding(encoding SourceEncoding) MaterializationRequest {
	r.encoding = encoding
	return r
}

// WithNewline selects an explicit newline policy.
func (r MaterializationRequest) WithNewline(newline NewlinePolicy) MaterializationRequest {
	r.newline = newline
	return r
}

// WithMappingPolicy selects explicit ordered-mapping behavior.
func (r MaterializationRequest) WithMappingPolicy(policy MappingPolicy) MaterializationRequest {
	r.mappingPolicy = policy
	return r
}

// WithLimits replaces the immutable materialization limits.
func (r MaterializationRequest) WithLimits(limits MaterializationLimits) MaterializationRequest {
	r.limits = limits
	return r
}

// TargetProfile returns the exact target Profile.
func (r MaterializationRequest) TargetProfile() ProfileId { return r.targetProfile }

// Style returns the exact versioned target style.
func (r MaterializationRequest) Style() MaterializationStyleId { return r.style }

// Encoding returns the selected output encoding.
func (r MaterializationRequest) Encoding() SourceEncoding { return r.encoding }

// Newline returns the selected newline behavior.
func (r MaterializationRequest) Newline() NewlinePolicy { return r.newline }

// MappingPolicy returns the ordered-mapping behavior.
func (r MaterializationRequest) MappingPolicy() MappingPolicy { return r.mappingPolicy }

// Representability returns the representability behavior.
func (r MaterializationRequest) Representability() RepresentabilityPolicy {
	return r.representability
}

// Limits returns the resource limits.
func (r MaterializationRequest) Limits() MaterializationLimits { return r.limits }
