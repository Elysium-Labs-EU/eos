package manager

import "errors"

var (
	ErrServiceAlreadyRegistered = errors.New("service already registered")
	ErrServiceNotRunning        = errors.New("service not running")
	ErrServiceNotRegistered     = errors.New("service not registered")
	ErrProcessNotFound          = errors.New("not found")
	ErrAlreadyRunning           = errors.New("already running")
)

const (
	CodeServiceAlreadyRegistered = "service_already_registered"
	CodeServiceNotRunning        = "service_not_running"
	CodeServiceNotRegistered     = "service_not_registered"
	CodeProcessNotFound          = "process_not_found"
	CodeAlreadyRunning           = "already_running"
)

var errCodeMap = map[string]error{
	CodeServiceAlreadyRegistered: ErrServiceAlreadyRegistered,
	CodeServiceNotRunning:        ErrServiceNotRunning,
	CodeServiceNotRegistered:     ErrServiceNotRegistered,
	CodeProcessNotFound:          ErrProcessNotFound,
	CodeAlreadyRunning:           ErrAlreadyRunning,
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
