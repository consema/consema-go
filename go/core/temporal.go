package core

import (
	"math/big"
)

// This file implements the four temporal PortableValue kinds (配置内容统一处理
// 标准与 Rust 参考实现.md §10.6; consema-rs/consema-core/src/value.rs):
// Date, Time, LocalDateTime, and OffsetDateTime. Validation mirrors the Rust
// constructors exactly:
//
//   - Date uses the proleptic Gregorian calendar with astronomical year
//     numbering; the leap rule operates on the year's absolute magnitude
//     (value.rs);
//   - Time rejects leap seconds and 24:00:00, and requires the fractional
//     second to be an exact finite decimal in [0, 1)
//     (value.rs, is_fraction at 337-352);
//   - OffsetDateTime requires |offset_seconds| < 24 * 60 * 60
//     (value.rs).
//
// The zero value of Date (and of LocalDateTime/OffsetDateTime containing it)
// is not a valid calendar date; constructing one and passing it to the codec
// returns a typed PVCEError with ErrInvalidValue. The zero value of Time is a
// valid time (00:00:00.0).

// Date is a proleptic Gregorian date with astronomical year numbering
// (配置内容统一处理标准与 Rust 参考实现.md §10.6; the Rust Date,
// consema-rs/consema-core/src/value.rs). The year is an arbitrary
// precision signed integer; month is 1-12 and day is valid for the month and
// the year's leap status.
type Date struct {
	year  Integer
	month uint8
	day   uint8
}

// NewDate validates and constructs a date. month must be 1-12 and day must
// be valid for the month under the proleptic Gregorian leap rule applied to
// the absolute magnitude of year (so year -400 is a leap year and year -100
// is not). A nil year is treated as zero. An invalid calendar date returns a
// *PVCEError with ErrInvalidTemporal.
func NewDate(year *big.Int, month, day uint8) (Date, error) {
	date := Date{year: NewInteger(year), month: month, day: day}
	if !dateFieldsValid(date.year.safeValue(), month, day) {
		return Date{}, &PVCEError{Kind: ErrInvalidTemporal}
	}
	return date, nil
}

// Year returns a copy of the astronomical year.
func (d Date) Year() Integer { return NewInteger(d.year.safeValue()) }

// Month returns the month number, 1-12.
func (d Date) Month() uint8 { return d.month }

// Day returns the day number.
func (d Date) Day() uint8 { return d.day }

// Kind implements Value.
func (Date) Kind() Kind { return KindDate }

func (Date) isValue() {}

// Time is a wall-clock time without leap seconds or 24:00:00
// (配置内容统一处理标准与 Rust 参考实现.md §10.6; the Rust Time,
// consema-rs/consema-core/src/value.rs). The fractional second is an
// exact finite decimal in [0, 1). The zero value is the valid time
// 00:00:00.0.
type Time struct {
	hour             uint8
	minute           uint8
	second           uint8
	fractionalSecond Decimal
}

// NewTime validates and constructs a time. hour must be 0-23, minute and
// second 0-59, and fraction an exact finite decimal in [0, 1) (zero
// coefficient, or coefficient × 10^exponent with coefficient >= 1 and
// decimal digits + exponent <= 0, mirroring the Rust Time::new is_fraction
// rule, consema-rs/consema-core/src/value.rs). An invalid time returns a
// *PVCEError with ErrInvalidTemporal.
func NewTime(hour, minute, second uint8, fraction Decimal) (Time, error) {
	time := Time{hour: hour, minute: minute, second: second, fractionalSecond: fraction}
	if !timeFieldsValid(hour, minute, second, fraction) {
		return Time{}, &PVCEError{Kind: ErrInvalidTemporal}
	}
	return time, nil
}

// Hour returns the hour, 0-23.
func (t Time) Hour() uint8 { return t.hour }

// Minute returns the minute, 0-59.
func (t Time) Minute() uint8 { return t.minute }

// Second returns the second, 0-59.
func (t Time) Second() uint8 { return t.second }

// FractionalSecond returns the exact fractional second in [0, 1).
func (t Time) FractionalSecond() Decimal { return t.fractionalSecond }

// Kind implements Value.
func (Time) Kind() Kind { return KindTime }

func (Time) isValue() {}

// LocalDateTime is a Date plus a Time without any offset
// (配置内容统一处理标准与 Rust 参考实现.md §10.6; the Rust LocalDateTime,
// consema-rs/consema-core/src/value.rs). It is not a timestamp.
type LocalDateTime struct {
	date Date
	time Time
}

// NewLocalDateTime creates a local date-time. The zero value is invalid (it
// contains the invalid zero Date).
func NewLocalDateTime(date Date, time Time) LocalDateTime {
	return LocalDateTime{date: date, time: time}
}

// Date returns the date part.
func (l LocalDateTime) Date() Date { return l.date }

// Time returns the time part.
func (l LocalDateTime) Time() Time { return l.time }

// Kind implements Value.
func (LocalDateTime) Kind() Kind { return KindLocalDateTime }

func (LocalDateTime) isValue() {}

// OffsetDateTime is a LocalDateTime plus a fixed UTC offset in whole
// seconds (配置内容统一处理标准与 Rust 参考实现.md §10.6; the Rust
// OffsetDateTime, consema-rs/consema-core/src/value.rs). The offset
// magnitude is less than 24 hours; the value locates the timeline but never
// carries an IANA region timezone.
type OffsetDateTime struct {
	local         LocalDateTime
	offsetSeconds int32
}

// NewOffsetDateTime validates and constructs an offset date-time. The
// offset magnitude must be less than 24 * 60 * 60 seconds; otherwise a
// *PVCEError with ErrInvalidTemporal is returned.
func NewOffsetDateTime(local LocalDateTime, offsetSeconds int32) (OffsetDateTime, error) {
	if offsetSeconds >= 24*60*60 || offsetSeconds <= -24*60*60 {
		return OffsetDateTime{}, &PVCEError{Kind: ErrInvalidTemporal}
	}
	return OffsetDateTime{local: local, offsetSeconds: offsetSeconds}, nil
}

// Local returns the local date-time fields.
func (o OffsetDateTime) Local() LocalDateTime { return o.local }

// OffsetSeconds returns the fixed UTC offset in seconds.
func (o OffsetDateTime) OffsetSeconds() int32 { return o.offsetSeconds }

// Kind implements Value.
func (OffsetDateTime) Kind() Kind { return KindOffsetDateTime }

func (OffsetDateTime) isValue() {}

// dateFieldsValid reports whether the fields form a valid proleptic Gregorian
// date under astronomical year numbering (the Rust Date::new checks,
// consema-rs/consema-core/src/value.rs). The leap rule uses the absolute
// magnitude of the year.
func dateFieldsValid(year *big.Int, month, day uint8) bool {
	if month < 1 || month > 12 {
		return false
	}
	magnitude := new(big.Int).Abs(year)
	remains := func(divisor int64) bool {
		var remainder big.Int
		remainder.Mod(magnitude, big.NewInt(divisor))
		return remainder.Sign() == 0
	}
	leap := remains(4) && (!remains(100) || remains(400))
	var maxDay uint8
	switch {
	case month == 2 && leap:
		maxDay = 29
	case month == 2:
		maxDay = 28
	case month == 4 || month == 6 || month == 9 || month == 11:
		maxDay = 30
	default:
		maxDay = 31
	}
	return day >= 1 && day <= maxDay
}

// timeFieldsValid reports whether the fields form a valid Time (the Rust
// Time::new checks, consema-rs/consema-core/src/value.rs).
func timeFieldsValid(hour, minute, second uint8, fraction Decimal) bool {
	return hour <= 23 && minute <= 59 && second <= 59 && isFraction(fraction)
}

// isFraction reports whether the canonical decimal represents a value in
// [0, 1) (the Rust Decimal::is_fraction,
// consema-rs/consema-core/src/value.rs): a non-negative coefficient, and
// either zero coefficient or an exponent small enough that the coefficient's
// decimal digits plus the exponent is <= 0.
func isFraction(d Decimal) bool {
	coefficient := d.safeCoefficient()
	if coefficient.Sign() < 0 {
		return false
	}
	if coefficient.Sign() == 0 {
		return true
	}
	exponent := d.safeExponent()
	if exponent.Sign() >= 0 {
		return false
	}
	digits := big.NewInt(int64(len(new(big.Int).Abs(coefficient).String())))
	var sum big.Int
	sum.Add(exponent, digits)
	return sum.Sign() <= 0
}

// dateValid reports whether a completed Date value carries valid calendar
// fields (used by the codec to reject zero-value Dates).
func dateValid(d Date) bool {
	return dateFieldsValid(d.year.safeValue(), d.month, d.day)
}

// timeValid reports whether a completed Time value carries valid fields
// (used by the codec; the zero value is valid).
func timeValid(t Time) bool {
	return timeFieldsValid(t.hour, t.minute, t.second, t.fractionalSecond)
}

// localValid reports whether a completed LocalDateTime carries a valid date
// part.
func localValid(l LocalDateTime) bool {
	return dateValid(l.date)
}
