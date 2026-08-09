package helpers

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

var ErrAPICommandFailed = errors.New("api command failed")

// ErrCommandFailed is returned by interactive (non-API) commands after they've
// already printed a human-readable error, so RunE still yields a non-nil error
// and the process exits non-zero without cobra or root's Execute double-printing
// the full message.
var ErrCommandFailed = errors.New("command failed")

func WriteJSON(cmd *cobra.Command, v any) error {
	out, err := json.Marshal(v)
	if err != nil {
		return WriteJSONErr(cmd, err)
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(out))
	return nil
}

func WriteJSONErr(cmd *cobra.Command, err error) error {
	out, _ := json.Marshal(map[string]string{"error": err.Error()})
	cmd.PrintErr(string(out) + "\n")
	return ErrAPICommandFailed
}

// WriteJSONErrCode is WriteJSONErr plus a stable "code" field, so a
// script-driven caller can branch on the failure type without parsing the
// human-readable "error" string.
func WriteJSONErrCode(cmd *cobra.Command, err error, code string) error {
	out, _ := json.Marshal(map[string]string{"error": err.Error(), "code": code})
	cmd.PrintErr(string(out) + "\n")
	return ErrAPICommandFailed
}
