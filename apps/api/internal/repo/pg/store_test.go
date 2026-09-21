package pg_test

import (
	"testing"

	"doelab/api/internal/repo/pg"
	"doelab/api/internal/repo/repotest"
	"doelab/api/internal/service"
	"doelab/api/internal/testutil"
)

// The container is shared by every test of the package, so something has to
// outlive them all to stop it.
func TestMain(m *testing.M) { testutil.TestMain(m) }

func TestConformance(t *testing.T) {
	t.Parallel()
	repotest.Run(t, func(t *testing.T) service.Store {
		return pg.NewStore(testutil.Postgres(t))
	})
}
