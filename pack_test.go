package dagger_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudfoundry/dagger"
	"github.com/sclevine/spec"

	. "github.com/onsi/gomega"
)

func testPack(t *testing.T, when spec.G, it spec.S) {
	when("running pack", func() {
		var tmpDir string

		it.Before(func() {
			tmpDir = os.TempDir()

			var err error
			tmpDir, err = filepath.EvalSymlinks(tmpDir)
			Expect(err).ToNot(HaveOccurred())
		})

		it("should use default builder", func() {
			packer := dagger.NewPack(tmpDir)
			app, err := packer.Build()
			Expect(err).NotTo(HaveOccurred())

			Expect(app.BuildLogs()).To(ContainSubstring("[pack build  --builder cloudfoundry/cnb:cflinuxfs3]"))
		})

		it("should pack with given buildpacks and image-name", func() {
			packer := dagger.NewPack(tmpDir,
				dagger.SetBuildpacks("first-bp", "second-bp"),
				dagger.SetImage("test-pack-image"),
			)
			app, err := packer.Build()
			Expect(err).NotTo(HaveOccurred())

			Expect(app.BuildLogs()).To(ContainSubstring("[pack build test-pack-image --builder cloudfoundry/cnb:cflinuxfs3 --buildpack first-bp --buildpack second-bp]"))
		})

		it("should pack with given builder", func() {
			packer := dagger.NewPack(tmpDir,
				dagger.SetBuildpacks("first-bp", "second-bp"),
				dagger.SetBuilder("bionic"),
			)
			app, err := packer.Build()
			Expect(err).NotTo(HaveOccurred())

			Expect(app.BuildLogs()).To(ContainSubstring("[pack build  --builder cloudfoundry/cnb:bionic --buildpack first-bp --buildpack second-bp]"))
		})

		it("should pack in offline containers", func() {
			packer := dagger.NewPack(tmpDir,
				dagger.SetBuildpacks("first-bp"),
				dagger.SetImage("test-pack-image"),
				dagger.SetOffline(),
			)
			app, err := packer.Build()
			Expect(err).NotTo(HaveOccurred())

			Expect(app.BuildLogs()).To(ContainSubstring("[pack build test-pack-image --builder cloudfoundry/cnb:cflinuxfs3 --buildpack first-bp --network none --no-pull]"))
		})

		it("should pack with a given environment", func() {
			packer := dagger.NewPack(tmpDir,
				dagger.SetImage("test-pack-image"),
				dagger.SetEnv(map[string]string{
					"env1": "val1",
					"env2": "val2",
				}),
			)
			app, err := packer.Build()
			Expect(err).NotTo(HaveOccurred())
			Expect(app.BuildLogs()).To(ContainSubstring("[pack build test-pack-image --builder cloudfoundry/cnb:cflinuxfs3 -e env1=val1 -e env2=val2]"))
		})

		it("should not pack with given builder that is not supported", func() {
			packer := dagger.NewPack(tmpDir,
				dagger.SetBuildpacks("first-bp", "second-bp"),
				dagger.SetBuilder("not-supported"),
			)
			app, err := packer.Build()
			Expect(err.Error()).To(ContainSubstring("please use either 'bionic' or 'cflinuxfs3' as input keys to SetBuilder"))
			Expect(app).To(BeNil())
		})
	})
}
