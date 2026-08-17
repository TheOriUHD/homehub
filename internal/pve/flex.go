package pve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// PVE encodes many scalars inconsistently across endpoints and versions:
// numbers may arrive as JSON strings, booleans as 0/1 (number or string).
// The Flex* types accept number, string, or bool and normalize.

// FlexInt64 is an int64 that also decodes from strings, floats, and bools.
type FlexInt64 int64

// Int64 returns the value as a plain int64.
func (f FlexInt64) Int64() int64 { return int64(f) }

// UnmarshalJSON implements json.Unmarshaler.
func (f *FlexInt64) UnmarshalJSON(b []byte) error {
	s, done, err := flexScalar(b)
	if done || err != nil {
		return err
	}
	switch s {
	case "", "false":
		*f = 0
		return nil
	case "true":
		*f = 1
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*f = FlexInt64(n)
		return nil
	}
	fl, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("pve: cannot decode %q as int64", s)
	}
	*f = FlexInt64(fl)
	return nil
}

// FlexFloat is a float64 that also decodes from strings and bools.
type FlexFloat float64

// Float64 returns the value as a plain float64.
func (f FlexFloat) Float64() float64 { return float64(f) }

// UnmarshalJSON implements json.Unmarshaler.
func (f *FlexFloat) UnmarshalJSON(b []byte) error {
	s, done, err := flexScalar(b)
	if done || err != nil {
		return err
	}
	switch s {
	case "", "false":
		*f = 0
		return nil
	case "true":
		*f = 1
		return nil
	}
	fl, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("pve: cannot decode %q as float64", s)
	}
	*f = FlexFloat(fl)
	return nil
}

// FlexBool is a bool that also decodes from 0/1 (number or string) and
// common truthy/falsy strings.
type FlexBool bool

// Bool returns the value as a plain bool.
func (f FlexBool) Bool() bool { return bool(f) }

// UnmarshalJSON implements json.Unmarshaler.
func (f *FlexBool) UnmarshalJSON(b []byte) error {
	s, done, err := flexScalar(b)
	if done || err != nil {
		return err
	}
	switch strings.ToLower(s) {
	case "", "0", "false", "no", "off":
		*f = false
		return nil
	case "1", "true", "yes", "on":
		*f = true
		return nil
	}
	if fl, err := strconv.ParseFloat(s, 64); err == nil {
		*f = fl != 0
		return nil
	}
	return fmt.Errorf("pve: cannot decode %q as bool", s)
}

// flexScalar reduces raw JSON to a trimmed scalar literal. done is true for
// JSON null, which leaves the receiver at its zero value per convention.
func flexScalar(raw []byte) (s string, done bool, err error) {
	t := string(bytes.TrimSpace(raw))
	if t == "null" {
		return "", true, nil
	}
	if len(t) > 0 && t[0] == '"' {
		var str string
		if err := json.Unmarshal([]byte(t), &str); err != nil {
			return "", false, err
		}
		return strings.TrimSpace(str), false, nil
	}
	return t, false, nil
}
