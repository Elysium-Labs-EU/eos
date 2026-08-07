package cmd

// Shared CLI output format strings, extracted to avoid duplicating the same
// literal across dozens of print call sites (sonar go:S1192).
const (
	fmtLabelMsg            = "%s %s\n\n"
	fmtLabelMsgLn          = "%s %s\n"
	fmtLabelTwoMsg         = "%s %s %s\n\n"
	fmtIndentLabelTwoMsg   = "  %s %s %s\n\n"
	fmtIndentLabelTwoMsgLn = "  %s %s %s\n"
	fmtIndentLabelMsgLn    = "  %s %s\n"
	fmtIndentLabelMsg      = "  %s %s\n\n"
	fmtHeading             = "%s\n\n"
	fmtIndentLabelAnyLn    = "  %s %v\n"
	fmtIndentLabelAny      = "  %s %v\n\n"
	fmtLabelKeyMsg         = "%s %s: %s\n\n"
)

// Shared CLI message/flag literals duplicated across command files.
const (
	msgRunHint              = "  run: "
	msgDaemonStartedBg      = "daemon started in background"
	msgDaemonWasNotRunning  = "daemon was not running"
	msgEosUpdatedTo         = "eos updated to"
	msgCheckDaemonLogs      = " → check daemon logs"
	flagDescSkipConfirm     = "skip all confirmation prompts (non-interactive mode)"
	headerUserAgent         = "User-Agent"
	systemctlDaemonReload   = "daemon-reload"
	logRunningSystemctl     = "running: systemctl %s"
	eosDaemonLogsCmdName    = "eos daemon logs"
	rcServiceCmdName        = "rc-service"
	eosRunFlagPathExample   = "eos run -f <path>"
	flagLogToFileAndConsole = "log-to-file-and-console"
)
