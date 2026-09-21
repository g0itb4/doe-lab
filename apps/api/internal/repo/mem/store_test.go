package mem_test

import (
	"testing"

	"doelab/api/internal/repo/mem"
	"doelab/api/internal/repo/repotest"
	"doelab/api/internal/service"
)

func TestConformance(t *testing.T) {
	t.Parallel()
	repotest.Run(t, func(*testing.T) service.Store { return mem.New() })
}
