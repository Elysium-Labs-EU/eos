package process

import (
	"deploy-cli/internal/manager"
	"deploy-cli/internal/testutil"
	"deploy-cli/internal/types"
	"strings"
	"testing"
)

func TestAllMethodsHandled(t *testing.T) {
	db, tempDir := testutil.SetupTestDB(t)
	manager := manager.NewLocalManager(db, tempDir)

	for method := range types.ValidMethods {
		t.Run(string(method), func(t *testing.T) {
			req := types.DaemonRequest{Method: method, Args: []string{"test"}}
			resp := executeRequest(manager, req)

			// Should NOT get "unknown method" error
			if !resp.Success && strings.Contains(resp.Error, "unknown method") {
				t.Errorf("Method %s not handled in switch", method)
			}
		})
	}
}
