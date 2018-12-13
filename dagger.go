package dagger

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/cloudfoundry/libcfbuildpack/helper"
	"github.com/buildpack/libbuildpack/buildpack"
)

const (
	originalImage = "cnb-pack-builder"
	//builderImage  = "cnb-acceptance-builder"
)

type Group struct {
	Buildpacks []buildpack.Info
}

type Buildpack struct {
	ID string
	URI string
}

type BuilderMetadata struct {
	Buildpacks []Buildpack
	Groups []Group
}

func ToTomlString(v interface{}) (string, error) {
	var b bytes.Buffer

	if err := toml.NewEncoder(&b).Encode(v); err != nil {
		return "", err
	}

	return b.String(), nil
}

func (b BuilderMetadata) writeToFile() (string, error) {
	builderFile, err := ioutil.TempFile("/tmp", "builder")
	if err != nil {
		return "", err
	}

	out, err := ToTomlString(b)
	if err != nil {
		return "", err
	}

	return builderFile.Name(), ioutil.WriteFile(builderFile.Name(), []byte(out), 0777)
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

func PackageBuildpack() (string, error) {
	cmd := exec.Command("../scripts/package.sh")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	r := regexp.MustCompile("Buildpack packaged into: (.*)")
	bpDir := r.FindStringSubmatch(string(out))[1]
	return bpDir, nil
}

func GetRemoteBuildpack(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	download, err := ioutil.TempFile("", "")
	if err != nil {
		return "", err
	}
	defer os.Remove(download.Name())

	_, err = io.Copy(download, resp.Body)
	if err != nil {
		return "", err
	}

	dest, err := ioutil.TempDir("", "")
	if err != nil {
		return "", err
	}

	return dest, helper.ExtractTarGz(download.Name(), dest, 0)
}

func PackBuild(appDir string, buildpacks ...string) (*App, error) {
	appImageName := randomString(16)

	cmd := exec.Command("pack", "build", appImageName, "--no-pull")
	for _, bp := range buildpacks {
		cmd.Args = append(cmd.Args, "--buildpack", bp)
	}
	cmd.Dir = appDir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return &App{imageName: appImageName}, nil
}

//NOTE: there are changes on master to dagger that have not been pulled to this branch
func Pack(appDir string, builderMetadata BuilderMetadata) (*App, error) {
	builderFile, err := builderMetadata.writeToFile()
	if err != nil {
		return nil, err
	}
	defer os.Remove(builderFile)

	//hardcoded stack, should eventually be changed
	cmd := exec.Command("pack", "create-builder", originalImage, "-b", builderFile, "-s", "org.cloudfoundry.stacks.cflinuxfs3")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	appImageName := randomString(16)

	cmd = exec.Command("pack", "build", appImageName, "--builder", originalImage, "--no-pull")
	cmd.Dir = appDir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	// FIXME: See Github issue https://github.com/buildpack/pack/issues/80
	cmd = exec.Command("docker", "run", "--user", "root", appImageName, "chmod", "0755", "/workspace/*")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	buf := &bytes.Buffer{}
	cmd = exec.Command("docker", "ps", "-lq")
	cmd.Stdout = buf
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	cmd = exec.Command("docker", "commit", strings.TrimSpace(buf.String()), appImageName)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	// End FIXME block

	return &App{imageName: appImageName, fixtureName: appDir}, nil
}

type App struct {
	imageName   string
	containerId string
	port        string
	fixtureName string
}

func (a *App) Start() error {
	buf := &bytes.Buffer{}

	// FIXME: Once Github issue https://github.com/buildpack/pack/issues/80 is fixed remove: "--entrypoint", "/lifecycle/launcher"
	cmd := exec.Command("docker", "run", "--entrypoint", "/lifecycle/launcher", "-d", "-P", a.imageName)
	cmd.Stdout = buf
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	a.containerId = buf.String()[:12]

	ticker := time.NewTicker(1 * time.Second)
	timeOut := time.After(40 * time.Second)
docker:
	for {
		select {
		case <-ticker.C:
			status, err := exec.Command("docker", "inspect", "-f", "{{.State.Health.Status}}", a.containerId).Output()
			if err != nil {
				return err
			}
			if strings.TrimSpace(string(status)) == "healthy" {
				break docker
			}
		case <-timeOut:
			return fmt.Errorf("timed out waiting for app : %s", a.fixtureName)
		}
	}

	cmd = exec.Command("docker", "container", "port", a.containerId)
	cmd.Stdout = buf
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	a.port = strings.TrimSpace(strings.Split(buf.String(), ":")[1])

	return nil
}

func (a *App) Destroy() error {
	if a.containerId == "" {
		return nil
	}

	cmd := exec.Command("docker", "stop", a.containerId)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	a.containerId = ""
	a.port = ""

	if a.imageName == "" {
		return nil
	}

	cmd = exec.Command("docker", "rmi", a.imageName, "-f")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	a.imageName = ""

	return nil
}

func (a *App) HTTPGet(path string) error {
	resp, err := http.Get("http://localhost:" + a.port + path)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("received bad response from application")
	}

	return nil
}

func randomString(n int) string {
	letterRunes := []rune("abcdefghijklmnopqrstuvwxyz")
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}
