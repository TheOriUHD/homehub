package pve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
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

// FlexPropString is a string that also decodes from a JSON object or number:
// newer Proxmox versions return structured objects for properties older ones
// (and the property-string config format) express as strings — e.g.
// prune-backups arrives as {"keep-last": 3} or "keep-last=3". Objects
// canonicalize to "key=value" pairs, sorted for stability.
type FlexPropString string

// String returns the canonical string form.
func (f FlexPropString) String() string { return string(f) }

// UnmarshalJSON implements json.Unmarshaler.
func (f *FlexPropString) UnmarshalJSON(b []byte) error {
	trimmed := strings.TrimSpace(string(b))
	if strings.HasPrefix(trimmed, "{") {
		var obj map[string]any
		if err := json.Unmarshal(b, &obj); err != nil {
			return err
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			switch v := obj[k].(type) {
			case float64:
				parts = append(parts, fmt.Sprintf("%s=%v", k, strconv.FormatFloat(v, 'f', -1, 64)))
			case bool:
				parts = append(parts, fmt.Sprintf("%s=%t", k, v))
			default:
				parts = append(parts, fmt.Sprintf("%s=%v", k, v))
			}
		}
		*f = FlexPropString(strings.Join(parts, ","))
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		// numbers and other scalars render as-is
		*f = FlexPropString(strings.Trim(trimmed, `"`))
		return nil
	}
	*f = FlexPropString(s)
	return nil
}
