// Package cmdnames is the single source of truth for eos's own command and
// action name strings. Both the human cobra tree (cmd/*.go) and its
// machine-readable mirror (cmd/api_*.go) build their Use: fields and
// cross-command hint text from these constants, so a sibling command's name
// can't drift between the two trees or between a hint and the command it
// actually points at.
package cmdnames

// Root is the eos binary's own invocation name, as it appears in hint text.
const Root = "eos"

// Top-level command names, shared between a command's own Use: field and any
// hint elsewhere that names it.
const (
	Add        = "add"
	Remove     = "remove"
	Run        = "run"
	Stop       = "stop"
	Status     = "status"
	Logs       = "logs"
	Update     = "update"
	Validate   = "validate"
	Info       = "info"
	System     = "system"
	Daemon     = "daemon"
	Reload     = "reload"
	Env        = "env"
	Completion = "completion"
	Init       = "init"
	API        = "api"
	Config     = "config"
)

// Daemon subcommand names.
const (
	DaemonStart  = "start"
	DaemonStop   = "stop"
	DaemonRemove = "remove"
	DaemonInfo   = "info"
	DaemonLogs   = "logs"
)

// System subcommand names.
const (
	SystemInfo      = "info"
	SystemStartup   = "startup"
	SystemUnstartup = "unstartup"
	SystemUpdate    = "update"
	SystemUninstall = "uninstall"
	SystemVersion   = "version"
)

// Config subcommand names.
const (
	ConfigShow     = "show"
	ConfigInit     = "init"
	ConfigValidate = "validate"
)

// Positional-arg placeholder text, shared by a command's human Use: field,
// its api_*.go mirror, and any hint that names the arg.
const (
	ArgPath        = "<path>"
	ArgServiceName = "<service-name>"
	ArgNewPath     = "<new-path>"
)

// UseAdd, UseRemove, etc. are the Use: field values shared by each command's
// human (cmd/*.go) and API (cmd/api_*.go) cobra definitions.
const (
	UseAdd      = Add + " " + ArgPath
	UseRemove   = Remove + " " + ArgServiceName
	UseStop     = Stop + " " + ArgServiceName
	UseUpdate   = Update + " " + ArgServiceName + " " + ArgNewPath
	UseInfo     = Info + " " + ArgServiceName
	UseLogs     = Logs + " " + ArgServiceName
	UseValidate = Validate + " " + ArgPath
	UseReload   = Reload + " " + ArgServiceName
)

// Hint* constants are full, ready-to-render "eos ..." invocations with no
// dynamic part, used directly in hint/tip text.
const (
	HintAdd             = Root + " " + Add + " " + ArgPath
	HintStatus          = Root + " " + Status
	HintDaemonStart     = Root + " " + Daemon + " " + DaemonStart
	HintDaemonInfo      = Root + " " + Daemon + " " + DaemonInfo
	HintDaemonLogs      = Root + " " + Daemon + " " + DaemonLogs
	HintSystemUpdate    = Root + " " + System + " " + SystemUpdate
	HintSystemUnstartup = Root + " " + System + " " + SystemUnstartup
	HintRunFlagPath     = Root + " " + Run + " -f " + ArgPath
	HintRunName         = Root + " " + Run + " <name>"
	HintUpdateArgs      = Root + " " + UseUpdate
	HintConfigShow      = Root + " " + Config + " " + ConfigShow
	HintConfigInit      = Root + " " + Config + " " + ConfigInit
)

// FmtHint* constants are "eos ..." invocation templates taking one %s
// argument (typically a service name), for use with fmt.Sprintf.
const (
	FmtHintRun     = Root + " " + Run + " %s"
	FmtHintRunFile = Root + " " + Run + " -f %s"
	FmtHintLogs    = Root + " " + Logs + " %s"
	FmtHintInfo    = Root + " " + Info + " %s"
	FmtHintRemove  = Root + " " + Remove + " %s"
	FmtHintStop    = Root + " " + Stop + " %s"
	FmtHintUpdate  = Root + " " + Update + " %s"
)
