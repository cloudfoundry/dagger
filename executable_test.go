package dagger_test

import (
	"bytes"
	"fmt"
	"github.com/buildpack/libbuildpack/logger"
	"github.com/cloudfoundry/dagger"
	executable "github.com/cloudfoundry/libbuildpack/cutlass/docker"
	"github.com/sclevine/spec"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
)

func testExecutable(t *testing.T, when spec.G, it spec.S) {


	when("using pack executable", func() {

		var tmpDir string
		var err error
		it.Before(func() {
			tmpDir = os.TempDir()
			tmpDir, err = filepath.EvalSymlinks(tmpDir)
			Expect(err).ToNot(HaveOccurred())
		})

		it("should run pack and produce output", func() {

			cmdOut := bytes.Buffer{}
			cmdErr := bytes.Buffer{}
			testLogger := logger.NewLogger(&cmdOut, &cmdErr)
			packExec := dagger.NewPackExecutable(testLogger)

			stdout, stderr, err := packExec.Execute(executable.ExecuteOptions{
				Dir: tmpDir,
			}, "a_real_rap_into_my_tech_stack")
			Expect(err).ToNot(HaveOccurred())
			Expect(stdout).To(ContainSubstring("Arguments: [pack a_real_rap_into_my_tech_stack]"))
			Expect(stdout).To(ContainSubstring(fmt.Sprintf("PWD: %s", tmpDir)))
			Expect(stderr).To(ContainSubstring("Pack output on stderr"))
		})

	})

}
