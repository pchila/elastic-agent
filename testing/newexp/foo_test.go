//go:build integration

package newexp

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDrill(t *testing.T) {
	// assert that values set in TestMain is available for tests
	assert.Equal(t, "This is not a drill.", pkgVar)
	assert.Contains(t, os.Environ(), "TEST_ENV_VAR=This is not a drill.")
}
