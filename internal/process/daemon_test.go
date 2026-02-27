package process

import (
	"strings"
	"testing"

	"eos/internal/database"
	"eos/internal/manager"
	"eos/internal/testutil"
	"eos/internal/types"
)

func TestAllMethodsHandled(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context())

	for method := range types.ValidMethods {
		t.Run(string(method), func(t *testing.T) {
			req := types.DaemonRequest{Method: method, Args: nil}
			resp := executeRequest(manager, req)

			// Should NOT get "unknown method" error
			if !resp.Success && strings.Contains(resp.Error, "unknown method") {
				t.Errorf("Method %s not handled in switch", method)
			}
		})
	}
}
