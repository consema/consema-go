package document

// FormationStatus is the closed two-value formation state of a successful
// document formation (document lib.rs; RFC 0016 §5.1 F10). The
// unexported field makes the set closed: no third value is constructible.
type FormationStatus struct {
	name string
}

// FormationStatusComplete means the entire syntax was formed without
// recovery.
var FormationStatusComplete = FormationStatus{name: "Complete"}

// FormationStatusRecovered means a complete snapshot with explicit
// recovery structure was formed.
var FormationStatusRecovered = FormationStatus{name: "Recovered"}

// String returns the stable status name.
func (s FormationStatus) String() string { return s.name }
