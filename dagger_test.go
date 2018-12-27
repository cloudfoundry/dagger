package dagger

import (
	"github.com/cloudfoundry/libcfbuildpack/buildpack"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/sclevine/spec/report"

	. "github.com/onsi/gomega"
	"github.com/sclevine/spec"
)

func TestIntegrationDagger(t *testing.T) {
	RegisterTestingT(t)
	spec.Run(t, "HTTPD", testDagger, spec.Report(report.Terminal{}))
}

func testDagger(t *testing.T, when spec.G, it spec.S) {
	when("Running Pack", func() {
		var app * App
		it.After(func(){
			if app != nil {
				app.Destroy()
			}
		})

		it("see successful command output", func() {
			appDir, err := ioutil.TempDir("", "")
			Expect(err).ToNot(HaveOccurred())
			cwd, err := os.Getwd()
			Expect(err).ToNot(HaveOccurred())


			builderMetadata := BuilderMetadata{
				Buildpacks:[]Buildpack {
					{
						ID: "io.buildpacks.samples.buildpack.hello-world",
						URI: filepath.Join(cwd ,"fixtures", "hello_world_buildpack"),
					},
				},
				Groups:[]Group{
					{
						[]buildpack.Info{
							{
								ID: "io.buildpacks.samples.buildpack.hello-world",
								Version: "0.0.1",
							},
						},
					},
				},
			}
			app, err = Pack(appDir, builderMetadata, CFLINUXFS3)
			Expect(err).ToNot(HaveOccurred())
		})

		it("should add text to failure output", func() {
			appDir, err := ioutil.TempDir("", "")
			Expect(err).ToNot(HaveOccurred())
			cwd, err := os.Getwd()
			Expect(err).ToNot(HaveOccurred())


			builderMetadata := BuilderMetadata{
				Buildpacks:[]Buildpack {
					{
						ID: "invalid.id",
						URI: filepath.Join(cwd ,"fixtures", "hello_world_buildpack"),
					},
				},
				Groups:[]Group{
					{
						[]buildpack.Info{
							{
								ID: "invalid.id",
								Version: "0.0.1",
							},
						},
					},
				},
			}

			app, err = Pack(appDir, builderMetadata, CFLINUXFS3)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Std out + error"))
		})
	})
}