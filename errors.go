package skillsruntime

import (
	"errors"
	"fmt"
)

const (
	CodeInvalidArgument       = "AGENT_SKILLS_INVALID_ARGUMENT"
	CodeSchemaUnsupported     = "AGENT_SKILLS_SCHEMA_UNSUPPORTED"
	CodeBundleInvalid         = "AGENT_SKILLS_BUNDLE_INVALID"
	CodeBundlePathForbidden   = "AGENT_SKILLS_BUNDLE_PATH_FORBIDDEN"
	CodeBundleSymlink         = "AGENT_SKILLS_BUNDLE_SYMLINK_FORBIDDEN"
	CodeDigestMismatch        = "AGENT_SKILLS_DIGEST_MISMATCH"
	CodeRegistryBusy          = "AGENT_SKILLS_REGISTRY_BUSY"
	CodePlanStale             = "AGENT_SKILLS_PLAN_STALE"
	CodePlanInvalid           = "AGENT_SKILLS_PLAN_INVALID"
	CodeConfirmationRequired  = "AGENT_SKILLS_CONFIRMATION_REQUIRED"
	CodeUnmanagedConflict     = "AGENT_SKILLS_UNMANAGED_CONFLICT"
	CodeDigestConflict        = "AGENT_SKILLS_DIGEST_CONFLICT"
	CodeUserDrift             = "AGENT_SKILLS_USER_DRIFT"
	CodeNotInstalled          = "AGENT_SKILLS_NOT_INSTALLED"
	CodeVersionMismatch       = "AGENT_SKILLS_VERSION_MISMATCH"
	CodeLegacyOwnerDetected   = "AGENT_SKILLS_LEGACY_OWNER_DETECTED"
	CodeTransactionIncomplete = "AGENT_SKILLS_TRANSACTION_INCOMPLETE"
	CodeIO                    = "AGENT_SKILLS_IO"
)

// Error is the stable machine-facing error contract returned by this module.
type Error struct {
	Code      string
	Message   string
	Path      string
	Action    string
	Retryable bool
	Err       error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Path != "" {
		return fmt.Sprintf("%s: %s: %s", e.Code, e.Message, e.Path)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

func newError(code, message, path string, err error) *Error {
	return &Error{Code: code, Message: message, Path: path, Err: err}
}

// ErrorCode returns the stable error code or an empty string for non-runtime errors.
func ErrorCode(err error) string {
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}
