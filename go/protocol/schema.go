package protocol

import (
	"math"
	"math/big"

	"consema.dev/consema/core"
)

// exactFields strictly validates a fixed-field object record: the value must
// be an Object, every field must be declared by the schema, every declared
// field must be present, and the fields must appear exactly in the canonical
// order. It returns the field values in schema order
// (consema-rs/crates/consema-protocol/src/schema.rs:16-53).
func exactFields(value core.Value, expected []string, path string) ([]core.Value, error) {
	object, ok := value.(*core.Object)
	if !ok {
		return nil, protocolError(KindWrongType, path, "expected Object")
	}
	entries := object.Entries()
	names := make([]string, len(entries))
	values := make([]core.Value, len(entries))
	for index, entry := range entries {
		names[index] = entry.Key
		values[index] = entry.Value
	}
	for _, name := range names {
		if !containsString(expected, name) {
			return nil, protocolError(KindUnknownField, path+"."+name, "field is not declared by the fixed schema")
		}
	}
	for _, name := range expected {
		if !containsString(names, name) {
			return nil, protocolError(KindMissingField, path+"."+name, "required field is absent")
		}
	}
	if !equalStrings(names, expected) {
		return nil, protocolError(KindSchemaMismatch, path, "fields are not in canonical order")
	}
	return values, nil
}

// schemaFields validates a fixed-field record whose first field is the
// schema discriminator and returns all field values
// (consema-rs/crates/consema-protocol/src/schema.rs:55-70).
func schemaFields(value core.Value, schema string, expected []string, path string) ([]core.Value, error) {
	fields, err := exactFields(value, expected, path)
	if err != nil {
		return nil, err
	}
	observed, err := stringOf(fields[0], path+".schema")
	if err != nil {
		return nil, err
	}
	if observed != schema {
		return nil, protocolError(KindSchemaMismatch, path+".schema", "expected "+schema)
	}
	return fields, nil
}

// stringOf reads a String field (crate::schema::string).
func stringOf(value core.Value, path string) (string, error) {
	text, ok := value.(core.String)
	if !ok {
		return "", protocolError(KindWrongType, path, "expected String")
	}
	return string(text), nil
}

// booleanOf reads a Boolean field (crate::schema::boolean).
func booleanOf(value core.Value, path string) (bool, error) {
	boolean, ok := value.(core.Boolean)
	if !ok {
		return false, protocolError(KindWrongType, path, "expected Boolean")
	}
	return bool(boolean), nil
}

// sequenceOf reads a Sequence field (crate::schema::sequence).
func sequenceOf(value core.Value, path string) ([]core.Value, error) {
	array, ok := value.(*core.Array)
	if !ok {
		return nil, protocolError(KindWrongType, path, "expected Sequence")
	}
	return array.Items(), nil
}

// unsigned32 reads an Integer field that must fit an unsigned 32-bit range
// (crate::schema::unsigned_u32).
func unsigned32(value core.Value, path string) (uint32, error) {
	integer, ok := value.(core.Integer)
	if !ok {
		return 0, protocolError(KindWrongType, path, "expected Integer")
	}
	number := integer.Int()
	if !number.IsInt64() || number.Sign() < 0 || number.Int64() > int64(^uint32(0)) {
		return 0, protocolError(KindInvalidValue, path, "expected an unsigned 32-bit Integer")
	}
	return uint32(number.Int64()), nil
}

// unsigned64 reads an Integer field that must fit an unsigned 64-bit range
// (crate::schema::unsigned_u64).
func unsigned64(value core.Value, path string) (uint64, error) {
	integer, ok := value.(core.Integer)
	if !ok {
		return 0, protocolError(KindWrongType, path, "expected Integer")
	}
	number := integer.Int()
	if number.Sign() < 0 || number.BitLen() > 64 {
		return 0, protocolError(KindInvalidValue, path, "expected an unsigned 64-bit Integer")
	}
	return number.Uint64(), nil
}

// integerValue builds the Integer record for a u64 (crate::schema::integer_u64).
func integerValue(value uint64) core.Value {
	return core.NewInteger(new(big.Int).SetUint64(value))
}

// bigInt32 builds the Integer record for an i32 (crate::schema::signed_i32).
func bigInt32(value int32) *big.Int {
	return big.NewInt(int64(value))
}

// signed32 reads an Integer field that must fit a signed 32-bit range
// (crate::schema::signed_i32).
func signed32(value core.Value, path string) (int32, error) {
	integer, ok := value.(core.Integer)
	if !ok {
		return 0, protocolError(KindWrongType, path, "expected Integer")
	}
	number := integer.Int()
	if !number.IsInt64() || number.Int64() < math.MinInt32 || number.Int64() > math.MaxInt32 {
		return 0, protocolError(KindInvalidValue, path, "expected a signed 32-bit Integer")
	}
	return int32(number.Int64()), nil
}

// nullableString encodes an optional string as String or Null
// (crate::schema::nullable_string).
func nullableString(value *string) core.Value {
	if value == nil {
		return core.NullValue()
	}
	return core.String(*value)
}

// optionalString decodes an optional string: Null yields None, any other
// value must be a String (crate::schema::optional_string).
func optionalString(value core.Value, path string) (*string, error) {
	if _, ok := value.(core.Null); ok {
		return nil, nil
	}
	text, err := stringOf(value, path)
	if err != nil {
		return nil, err
	}
	return &text, nil
}

// containsString reports whether the slice contains the element.
func containsString(slice []string, element string) bool {
	for _, candidate := range slice {
		if candidate == element {
			return true
		}
	}
	return false
}

// equalStrings reports whether two slices are element-wise equal.
func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
