package test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-cd/v3/util/regex"
)

const SUBPROC_ENV_VAR = "SANDBOX_TEST_SUBPROCESS"

func isTestInSubproc() bool {
	return os.Getenv(SUBPROC_ENV_VAR) != ""
}

func nameToPattern(name string) string {
	buf := strings.Builder{}
	parts := strings.Split(name, "/")
	for idx, part := range parts {
		if idx > 0 {
			buf.WriteByte('/')
		}
		buf.WriteString("^")
		buf.WriteString(regexp.QuoteMeta(part))
		buf.WriteString("$")
	}
	return buf.String()
}

// RunInSubprocess allows to run a test in a subprocess
// t test state
// disableCoverage disable coverage processing (it might not work when testing sandbox operations in a subprocess)
// testfunc a closure function that should use t from the outer scope
func RunInSubprocess(t *testing.T, disableCoverage bool, testfunc func()) {
	t.Helper()
	if os.Getenv(SUBPROC_ENV_VAR) != "" {
		t.Logf("start running test %q in subprocess PID=%d PPID=%d\n", t.Name(), os.Getpid(), os.Getppid())
		testfunc()
		t.Logf("finished running test %q in subprocess", t.Name())
	} else {
		pid := os.Getpid()
		t.Logf("running test %q in parent process PID=%d", t.Name(), pid)
		// Parent process: re-exec this same test binary, running only this test.
		t.Logf("called with args %v", os.Args)
		cmdArgs := []string{}
		for _, arg := range os.Args[1:] {
			if strings.HasPrefix(arg, "-test.run=") || arg == "-test.paniconexit0" {
				continue
			}
			if disableCoverage &&
				(strings.HasPrefix(arg, "-test.gocoverdir=") ||
					strings.HasPrefix(arg, "-test.coverprofile=")) {
				continue
			}
			if !strings.HasPrefix(arg, "-test.") {
				continue
			}
			cmdArgs = append(cmdArgs, arg)
		}
		cmdArgs = append(cmdArgs, "-test.run="+nameToPattern(t.Name()))

		cmdEnv := []string{}
		for _, entry := range os.Environ() {
			if disableCoverage && strings.HasPrefix(entry, "GOCOVERDIR=") {
				continue
			}
			cmdEnv = append(cmdEnv, entry)
		}
		cmdEnv = append(cmdEnv, fmt.Sprintf("%s=%d", SUBPROC_ENV_VAR, pid))

		cmd := exec.CommandContext(t.Context(), os.Args[0], cmdArgs...)
		cmd.Env = cmdEnv
		t.Logf("executing binary %q with args %v", os.Args[0], cmdArgs)
		t.Logf("the environment is %v\n", cmdEnv)
		output, err := cmd.CombinedOutput()
		outputStr := string(output)
		t.Logf("subprocess output:\n%s", outputStr)
		if err != nil {
			exitErr := &exec.ExitError{}
			if errors.As(err, &exitErr) {
				if !disableCoverage || !regex.Match("error setting GOCOVERDIR: bad os.MkdirTemp return: mkdir /[^\\s]+: permission denied", outputStr) {
					assert.Fail(t, fmt.Sprintf("expected subprocess exit code 0, but got %d", exitErr.ExitCode()))
				}
			} else {
				assert.Fail(t, fmt.Sprintf("error running test in subprocess: %v", err))
			}
		} else {
			t.Logf("test %s subprocess execution succeeded\n", t.Name())
		}
	}
}
