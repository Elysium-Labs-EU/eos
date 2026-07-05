package helpers

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

var ErrAPICommandFailed = errors.New("api command failed")

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
