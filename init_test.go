package dagger_test

import (
	"github.com/onsi/gomega/gexec"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"
)

func TestUnitCloudNative(t *testing.T) {
	suite := spec.New("cloudnative", spec.Report(report.Terminal{}))

	var err error
	var fakePackCLI string
	var fakeDockerCLI string
	var existingPath string


	suite.Before(func(t *testing.T) {
		RegisterTestingT(t)

		existingPath = os.Getenv("PATH")

		fakeDockerCLI, err = gexec.Build("github.com/cloudfoundry/dagger/fakes/docker")
		Expect(err).NotTo(HaveOccurred())

		fakePackCLI, err = gexec.Build("github.com/cloudfoundry/dagger/fakes/pack")
		Expect(err).NotTo(HaveOccurred())

		newPath := strings.Join([]string{
			filepath.Dir(fakeDockerCLI),
			filepath.Dir(fakePackCLI),
		}, string(os.PathListSeparator))
		Expect(os.Setenv("PATH", newPath)).To(Succeed())
	})

	suite.After(func(t *testing.T) {
		Expect(os.Setenv("PATH", existingPath)).To(Succeed())
		gexec.CleanupBuildArtifacts()
	})

	suite("Pack", testPack)

	suite.Run(t)
}
