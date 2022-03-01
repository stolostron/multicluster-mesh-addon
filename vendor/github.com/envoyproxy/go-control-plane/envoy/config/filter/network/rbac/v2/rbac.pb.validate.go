// Code generated by protoc-gen-validate. DO NOT EDIT.
// source: envoy/config/filter/network/rbac/v2/rbac.proto

package rbacv2

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/protobuf/types/known/anypb"
)

// ensure the imports are used
var (
	_ = bytes.MinRead
	_ = errors.New("")
	_ = fmt.Print
	_ = utf8.UTFMax
	_ = (*regexp.Regexp)(nil)
	_ = (*strings.Reader)(nil)
	_ = net.IPv4len
	_ = time.Duration(0)
	_ = (*url.URL)(nil)
	_ = (*mail.Address)(nil)
	_ = anypb.Any{}
	_ = sort.Sort
)

// Validate checks the field values on RBAC with the rules defined in the proto
// definition for this message. If any rules are violated, the first error
// encountered is returned, or nil if there are no violations.
func (m *RBAC) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on RBAC with the rules defined in the
// proto definition for this message. If any rules are violated, the result is
// a list of violation errors wrapped in RBACMultiError, or nil if none found.
func (m *RBAC) ValidateAll() error {
	return m.validate(true)
}

func (m *RBAC) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	if all {
		switch v := interface{}(m.GetRules()).(type) {
		case interface{ ValidateAll() error }:
			if err := v.ValidateAll(); err != nil {
				errors = append(errors, RBACValidationError{
					field:  "Rules",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		case interface{ Validate() error }:
			if err := v.Validate(); err != nil {
				errors = append(errors, RBACValidationError{
					field:  "Rules",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		}
	} else if v, ok := interface{}(m.GetRules()).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return RBACValidationError{
				field:  "Rules",
				reason: "embedded message failed validation",
				cause:  err,
			}
		}
	}

	if all {
		switch v := interface{}(m.GetShadowRules()).(type) {
		case interface{ ValidateAll() error }:
			if err := v.ValidateAll(); err != nil {
				errors = append(errors, RBACValidationError{
					field:  "ShadowRules",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		case interface{ Validate() error }:
			if err := v.Validate(); err != nil {
				errors = append(errors, RBACValidationError{
					field:  "ShadowRules",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		}
	} else if v, ok := interface{}(m.GetShadowRules()).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return RBACValidationError{
				field:  "ShadowRules",
				reason: "embedded message failed validation",
				cause:  err,
			}
		}
	}

	if len(m.GetStatPrefix()) < 1 {
		err := RBACValidationError{
			field:  "StatPrefix",
			reason: "value length must be at least 1 bytes",
		}
		if !all {
			return err
		}
		errors = append(errors, err)
	}

	// no validation rules for EnforcementType

	if len(errors) > 0 {
		return RBACMultiError(errors)
	}
	return nil
}

// RBACMultiError is an error wrapping multiple validation errors returned by
// RBAC.ValidateAll() if the designated constraints aren't met.
type RBACMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m RBACMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m RBACMultiError) AllErrors() []error { return m }

// RBACValidationError is the validation error returned by RBAC.Validate if the
// designated constraints aren't met.
type RBACValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e RBACValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e RBACValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e RBACValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e RBACValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e RBACValidationError) ErrorName() string { return "RBACValidationError" }

// Error satisfies the builtin error interface
func (e RBACValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sRBAC.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = RBACValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = RBACValidationError{}
