package database_test

import (
	"fmt"
	"testing"

	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/testutil"
	"codeberg.org/Elysium_Labs/eos/internal/types"
)

var benchSizes = []int{10, 100, 1000}

func BenchmarkGetAllServiceCatalogEntries(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			db, _, _ := testutil.SetupTestDB(b, database.MigrationsFS, database.MigrationsPath)
			testutil.SeedServiceCatalog(b, b.Context(), db, n)
			b.ResetTimer()
			for b.Loop() {
				_, _ = db.GetAllServiceCatalogEntries(b.Context())
			}
		})
	}
}

func BenchmarkGetServiceCatalogEntry(b *testing.B) {
	db, _, _ := testutil.SetupTestDB(b, database.MigrationsFS, database.MigrationsPath)
	testutil.SeedServiceCatalog(b, b.Context(), db, 100)
	b.ResetTimer()
	for b.Loop() {
		_, _ = db.GetServiceCatalogEntry(b.Context(), "svc-50")
	}
}

func BenchmarkIsServiceRegistered(b *testing.B) {
	db, _, _ := testutil.SetupTestDB(b, database.MigrationsFS, database.MigrationsPath)
	testutil.SeedServiceCatalog(b, b.Context(), db, 100)
	b.ResetTimer()
	for b.Loop() {
		_, _ = db.IsServiceRegistered(b.Context(), "svc-50")
	}
}

func BenchmarkGetAllServiceInstances(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			db, _, _ := testutil.SetupTestDB(b, database.MigrationsFS, database.MigrationsPath)
			entries := testutil.SeedServiceCatalog(b, b.Context(), db, n)
			names := make([]string, len(entries))
			for i, e := range entries {
				names[i] = e.Name
			}
			testutil.SeedServiceInstances(b, b.Context(), db, names)
			b.ResetTimer()
			for b.Loop() {
				_, _ = db.GetAllServiceInstances(b.Context())
			}
		})
	}
}

func BenchmarkGetServiceInstance(b *testing.B) {
	db, _, _ := testutil.SetupTestDB(b, database.MigrationsFS, database.MigrationsPath)
	entries := testutil.SeedServiceCatalog(b, b.Context(), db, 100)
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	testutil.SeedServiceInstances(b, b.Context(), db, names)
	b.ResetTimer()
	for b.Loop() {
		_, _ = db.GetServiceInstance(b.Context(), "svc-50")
	}
}

func BenchmarkGetProcessHistoryEntriesByServiceName(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			db, _, _ := testutil.SetupTestDB(b, database.MigrationsFS, database.MigrationsPath)
			testutil.SeedServiceCatalog(b, b.Context(), db, 1)
			testutil.SeedServiceInstances(b, b.Context(), db, []string{"svc-0"})
			testutil.SeedProcessHistory(b, b.Context(), db, "svc-0", n)
			b.ResetTimer()
			for b.Loop() {
				_, _ = db.GetProcessHistoryEntriesByServiceName(b.Context(), "svc-0")
			}
		})
	}
}

func BenchmarkGetMostRecentProcessHistoryEntryByName(b *testing.B) {
	db, _, _ := testutil.SetupTestDB(b, database.MigrationsFS, database.MigrationsPath)
	testutil.SeedServiceCatalog(b, b.Context(), db, 1)
	testutil.SeedServiceInstances(b, b.Context(), db, []string{"svc-0"})
	testutil.SeedProcessHistory(b, b.Context(), db, "svc-0", 100)
	b.ResetTimer()
	for b.Loop() {
		_, _ = db.GetMostRecentProcessHistoryEntryByName(b.Context(), "svc-0")
	}
}

func BenchmarkGetProcessHistoryEntryByPGID(b *testing.B) {
	db, _, _ := testutil.SetupTestDB(b, database.MigrationsFS, database.MigrationsPath)
	testutil.SeedServiceCatalog(b, b.Context(), db, 1)
	testutil.SeedServiceInstances(b, b.Context(), db, []string{"svc-0"})
	testutil.SeedProcessHistory(b, b.Context(), db, "svc-0", 100)
	b.ResetTimer()
	for b.Loop() {
		// 10050 is the midpoint PGID: SeedProcessHistory defaults basePGID to 10000
		// and assigns basePGID+i for i in [0,100), so this hits the 51st seeded entry.
		_, _ = db.GetProcessHistoryEntryByPGID(b.Context(), 10050)
	}
}

func BenchmarkRegisterService(b *testing.B) {
	db, _, _ := testutil.SetupTestDB(b, database.MigrationsFS, database.MigrationsPath)
	b.ResetTimer()
	i := 0
	for b.Loop() {
		i++
		_ = db.RegisterService(b.Context(), fmt.Sprintf("svc-%d", i), "/srv/svc", "svc.yaml")
	}
}

func BenchmarkRegisterServiceInstance(b *testing.B) {
	db, _, _ := testutil.SetupTestDB(b, database.MigrationsFS, database.MigrationsPath)
	b.ResetTimer()
	i := 0
	for b.Loop() {
		i++
		_ = db.RegisterServiceInstance(b.Context(), fmt.Sprintf("svc-%d", i))
	}
}

func BenchmarkRegisterProcessHistoryEntry(b *testing.B) {
	db, _, _ := testutil.SetupTestDB(b, database.MigrationsFS, database.MigrationsPath)
	testutil.SeedServiceCatalog(b, b.Context(), db, 1)
	testutil.SeedServiceInstances(b, b.Context(), db, []string{"svc-0"})
	b.ResetTimer()
	i := 0
	for b.Loop() {
		i++
		_, _ = db.RegisterProcessHistoryEntry(b.Context(), 20000+i, 0, "svc-0", types.ProcessStateRunning)
	}
}
