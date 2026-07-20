package manager

import "errors"

var (
	ErrServiceAlreadyRegistered = errors.New("service already registered")
	ErrServiceNotRunning        = errors.New("service not running")
	ErrServiceNotRegistered     = errors.New("service not registered")
	ErrProcessNotFound          = errors.New("not found")
	ErrAlreadyRunning           = errors.New("already running")
	// ErrServiceNameCaseConflict is returned when a new service name collides
	// case-insensitively with an already-registered service. Such names are
	// distinct catalog rows but their log filenames alias onto a single file
	// on case-insensitive filesystems (e.g. macOS APFS), which silently
	// intermingles the two services' output. See GitHub issue #10.
	ErrServiceNameCaseConflict = errors.New("service name conflicts with an existing service differing only in letter case")
)

const (
	CodeServiceAlreadyRegistered = "service_already_registered"
	CodeServiceNotRunning        = "service_not_running"
	CodeServiceNotRegistered     = "service_not_registered"
	CodeProcessNotFound          = "process_not_found"
	CodeAlreadyRunning           = "already_running"
	CodeServiceNameCaseConflict  = "service_name_case_conflict"
)

var errCodeMap = map[string]error{
	CodeServiceAlreadyRegistered: ErrServiceAlreadyRegistered,
	CodeServiceNotRunning:        ErrServiceNotRunning,
	CodeServiceNotRegistered:     ErrServiceNotRegistered,
	CodeProcessNotFound:          ErrProcessNotFound,
	CodeAlreadyRunning:           ErrAlreadyRunning,
	CodeServiceNameCaseConflict:  ErrServiceNameCaseConflict,
}

// ErrorCode returns a machine-readable code for known sentinel errors, empty string otherwise.
func ErrorCode(err error) string {
	for code, sentinel := range errCodeMap {
		if errors.Is(err, sentinel) {
			return code
		}
	}
	return ""
}

// ErrorFromCode returns the sentinel error for a known code, nil otherwise.
func ErrorFromCode(code string) error {
	return errCodeMap[code]
}
