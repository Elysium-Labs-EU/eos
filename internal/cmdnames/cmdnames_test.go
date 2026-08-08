package cmdnames

import (
	"fmt"
	"strings"
	"testing"
)

// TestUseFieldsCarryTheirArgPlaceholder guards against exactly the drift
// issue #183 was filed over: a human Use: string and its api_*.go mirror
// silently disagreeing on the arg placeholder because each hardcoded its
// own literal instead of sharing one constant.
func TestUseFieldsCarryTheirArgPlaceholder(t *testing.T) {
	cases := []struct {
		name string
		use  string
		want string
	}{
		{"add", UseAdd, ArgPath},
		{"remove", UseRemove, ArgServiceName},
		{"stop", UseStop, ArgServiceName},
		{"update", UseUpdate, ArgServiceName},
		{"info", UseInfo, ArgServiceName},
		{"logs", UseLogs, ArgServiceName},
		{"validate", UseValidate, ArgPath},
		{"reload", UseReload, ArgServiceName},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(c.use, c.want) {
				t.Errorf("Use%s = %q, want it to contain placeholder %q", c.name, c.use, c.want)
			}
		})
	}
}

func TestUseUpdateCarriesBothPlaceholders(t *testing.T) {
	if !strings.Contains(UseUpdate, ArgServiceName) || !strings.Contains(UseUpdate, ArgNewPath) {
		t.Errorf("UseUpdate = %q, want both %q and %q", UseUpdate, ArgServiceName, ArgNewPath)
	}
}

// TestHintsStartWithRoot guards against a Hint constant losing its "eos "
// prefix, which would silently turn a hint into a bare subcommand fragment.
func TestHintsStartWithRoot(t *testing.T) {
	hints := map[string]string{
		"HintAdd":             HintAdd,
		"HintStatus":          HintStatus,
		"HintDaemonStart":     HintDaemonStart,
		"HintDaemonInfo":      HintDaemonInfo,
		"HintDaemonLogs":      HintDaemonLogs,
		"HintSystemUpdate":    HintSystemUpdate,
		"HintSystemUnstartup": HintSystemUnstartup,
		"HintRunFlagPath":     HintRunFlagPath,
		"HintRunName":         HintRunName,
		"HintUpdateArgs":      HintUpdateArgs,
	}
	for name, hint := range hints {
		if !strings.HasPrefix(hint, Root+" ") {
			t.Errorf("%s = %q, want prefix %q", name, hint, Root+" ")
		}
	}
}

// TestFmtHintsProduceRootPrefixedInvocation guards the fmt.Sprintf templates
// the same way, and confirms each still has exactly one verb to fill.
func TestFmtHintsProduceRootPrefixedInvocation(t *testing.T) {
	templates := map[string]string{
		"FmtHintRun":     FmtHintRun,
		"FmtHintRunFile": FmtHintRunFile,
		"FmtHintLogs":    FmtHintLogs,
		"FmtHintInfo":    FmtHintInfo,
		"FmtHintRemove":  FmtHintRemove,
		"FmtHintStop":    FmtHintStop,
		"FmtHintUpdate":  FmtHintUpdate,
	}
	for name, tmpl := range templates {
		if strings.Count(tmpl, "%s") != 1 {
			t.Errorf("%s = %q, want exactly one %%s verb", name, tmpl)
		}
		got := fmt.Sprintf(tmpl, "myservice")
		if !strings.HasPrefix(got, Root+" ") {
			t.Errorf("%s formatted = %q, want prefix %q", name, got, Root+" ")
		}
		if !strings.Contains(got, "myservice") {
			t.Errorf("%s formatted = %q, want it to contain the substituted arg", name, got)
		}
	}
}

func TestDaemonAndSystemSubcommandNamesAreNonEmpty(t *testing.T) {
	names := map[string]string{
		"DaemonStart":     DaemonStart,
		"DaemonStop":      DaemonStop,
		"DaemonRemove":    DaemonRemove,
		"DaemonInfo":      DaemonInfo,
		"DaemonLogs":      DaemonLogs,
		"SystemInfo":      SystemInfo,
		"SystemStartup":   SystemStartup,
		"SystemUnstartup": SystemUnstartup,
		"SystemUpdate":    SystemUpdate,
		"SystemUninstall": SystemUninstall,
		"SystemVersion":   SystemVersion,
	}
	for name, val := range names {
		if val == "" {
			t.Errorf("%s is empty", name)
		}
		if strings.Contains(val, " ") {
			t.Errorf("%s = %q, want a single bare subcommand word", name, val)
		}
	}
}
