package test

import (
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ensureSubProc(t *testing.T) {
	t.Helper()
	ppid := strconv.Itoa(os.Getppid())
	require.Equal(t, ppid, os.Getenv(SUBPROC_ENV_VAR), "Not running in subprocess of pid %s", ppid)
}

func TestNameToPattern(t *testing.T) {
	assert.Equal(t, "^abc$", nameToPattern("abc"))
	assert.Equal(t, "^abc$/^defg_hijk$", nameToPattern("abc/defg_hijk"))
	assert.Equal(t, "^abc$/^defg_hijk$/^lmn$", nameToPattern("abc/defg_hijk/lmn"))
	assert.Equal(t, "^a\\$\\*\\.\\^b$", nameToPattern("a$*.^b"))
}

func TestSubprocTestSuccess(t *testing.T) {
	runInSubproc := false
	RunInSubprocess(t, true, func() {
		runInSubproc = true
		ensureSubProc(t)
	})
	require.Equal(t, isTestInSubproc(), runInSubproc)
}

func TestSubprocTestCoverageSuccess(t *testing.T) {
	runInSubproc := false
	RunInSubprocess(t, false, func() {
		runInSubproc = true
		ensureSubProc(t)
	})
	require.Equal(t, isTestInSubproc(), runInSubproc)
}

func TestSubprocSubTestSuccess(t *testing.T) {
	count := 0
	t.Run("subtest one", func(t *testing.T) {
		runInSubproc := false
		RunInSubprocess(t, false, func() {
			count++
			runInSubproc = true
			ensureSubProc(t)
		})
		require.Equal(t, isTestInSubproc(), runInSubproc)
	})
	t.Run("subtest two", func(t *testing.T) {
		runInSubproc := false
		RunInSubprocess(t, false, func() {
			count++
			runInSubproc = true
			ensureSubProc(t)
		})
		require.Equal(t, isTestInSubproc(), runInSubproc)
	})
	t.Run("subtest (three) with a+*funny*??name$$$", func(t *testing.T) {
		runInSubproc := false
		RunInSubprocess(t, false, func() {
			count++
			runInSubproc = true
			ensureSubProc(t)
		})
		require.Equal(t, isTestInSubproc(), runInSubproc)
	})
	t.Run("subtest/four", func(t *testing.T) {
		runInSubproc := false
		RunInSubprocess(t, false, func() {
			count++
			runInSubproc = true
			ensureSubProc(t)
		})
		require.Equal(t, isTestInSubproc(), runInSubproc)
	})

	if isTestInSubproc() {
		require.Equal(t, 1, count)
	} else {
		require.Equal(t, 0, count)
	}
}

func TestSubprocTestSubproc(t *testing.T) {
	inSubproc := false
	t.Setenv(SUBPROC_ENV_VAR, strconv.Itoa(os.Getppid()))
	RunInSubprocess(t, false, func() {
		inSubproc = true
	})
	require.True(t, inSubproc, "testfunc was expected to run in this process, but it did not")
}
