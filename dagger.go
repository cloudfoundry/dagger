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
	"github.com/buildpack/libbuildpack/buildpack"
	"github.com/cloudfoundry/libcfbuildpack/helper"
)

const (
	originalImage = "cnb-pack-builder"
	builderImage  = "cnb-acceptance-builder"
)

const (
	CFLINUXFS3 = "org.cloudfoundry.stacks.cflinuxfs3"
	BIONIC     = "io.buildpacks.stacks.bionic"
)

type Group struct {
	Buildpacks []buildpack.Info
}

type Buildpack struct {
	ID  string
	URI string
}

type BuilderMetadata struct {
	Buildpacks []Buildpack
	Groups     []Group
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

	cmd := exec.Command("pack", "build", appImageName, "--no-pull", "--clear-cache")
	for _, bp := range buildpacks {
		cmd.Args = append(cmd.Args, "--buildpack", bp)
	}
	cmd.Dir = appDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return &App{imageName: appImageName}, nil
}

func Pack(appDir string, builderMetadata BuilderMetadata, stack string) (*App, error) {
	builderFile, err := builderMetadata.writeToFile()
	if err != nil {
		return nil, err
	}
	defer os.Remove(builderFile)

	//hardcoded stack, should eventually be changed
	cmd := exec.Command("pack", "create-builder", originalImage, "-b", builderFile, "-s", stack)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("Std out + error, %s: ", string(output)), err)
	}

	// FIX: this is necessary because permissions on `/buildpacks` are rwx for root user only ( but only happens in CI )
	cmd = exec.Command("docker", "run", "--user", "root", originalImage, "chmod", "0755", "/buildpacks")
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	buf := &bytes.Buffer{}
	cmd = exec.Command("docker", "ps", "-lq")
	cmd.Stdout = buf
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	cmd = exec.Command("docker", "commit", strings.TrimSpace(buf.String()), builderImage)
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	// END FIX

	appImageName := randomString(16)

	cmd = exec.Command("pack", "build", appImageName, "--builder", builderImage, "--no-pull", "--clear-cache")
	cmd.Dir = appDir
	output, err = cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("Std out + error, %s: ", string(output)), err)
	}

	return &App{imageName: appImageName, fixtureName: appDir, Env: make(map[string]string)}, nil
}

type App struct {
	imageName   string
	containerId string
	port        string
	fixtureName string
	healthCheck HealthCheck
	Env         map[string]string
}

type HealthCheck struct {
	command  string
	interval string
	timeout  string
}

func (a *App) SetHealthCheck(command, interval, timeout string) {
	a.healthCheck = HealthCheck{
		command:  command,
		interval: interval,
		timeout:  timeout,
	}
}

func (a *App) Start() error {
	buf := &bytes.Buffer{}

	args := []string{"run", "-d", "-P"}
	if a.healthCheck.command != "" {
		args = append(args, "--health-cmd", a.healthCheck.command)
	}

	if a.healthCheck.interval != "" {
		args = append(args, "--health-interval", a.healthCheck.interval)
	}

	if a.healthCheck.timeout != "" {
		args = append(args, "--health-timeout", a.healthCheck.timeout)
	}

	envTemplate := "%s=%s"
	for k, v := range a.Env {
		envString := fmt.Sprintf(envTemplate, k, v)
		args = append(args, "-e", envString)
	}

	args = append(args, a.imageName)

	cmd := exec.Command("docker", args...)
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

			if strings.TrimSpace(string(status)) == "unhealthy" {
				return fmt.Errorf("app failed to start : %s", a.fixtureName)
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
	if err := cmd.Run(); err != nil {
		return err
	}

	cmd = exec.Command("docker", "rm", a.containerId, "-f", "--volumes")
	if err := cmd.Run(); err != nil {
		return err
	}

	a.containerId = ""
	a.port = ""

	if a.imageName == "" {
		return nil
	}

	cmd = exec.Command("docker", "rmi", a.imageName, "-f")
	if err := cmd.Run(); err != nil {
		return err
	}

	cmd = exec.Command("docker", "image", "prune", "-f")
	if err := cmd.Run(); err != nil {
		return err
	}

	a.imageName = ""

	return nil
}

func (a *App) ContainerInfo() (cID string, imageID string, cacheID []string, e error) {
	volumes, err := GetCacheVolumes()
	if err != nil {
		return "", "", []string{}, err
	}

	return a.containerId, a.imageName, volumes, nil
}

func (a *App) ContainerLogs() (string, error) {
	cmd := exec.Command("docker", "logs", a.containerId)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	return string(output), nil
}

func GetCacheVolumes() ([]string, error) {
	cmd := exec.Command("docker", "volume", "ls", "-q")
	output, err := cmd.Output()
	if err != nil {
		return []string{}, err
	}

	outputArr := strings.Split(string(output), "\n")
	var finalVolumes []string
	for _, line := range outputArr {
		if strings.Contains(line, "pack-cache") {
			finalVolumes = append(finalVolumes, line)
		}
	}
	return outputArr, nil
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

func (a *App) HTTPGetSucceeds(path string) (response []byte, err error) {
	resp, err := http.Get("http://localhost:" + a.port + path)
	if err != nil {
		return response, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return response, fmt.Errorf("received bad response from application")
	}

	response, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return response, err
	}

	return response, nil
}

func randomString(n int) string {
	letterRunes := []rune("abcdefghijklmnopqrstuvwxyz")
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}
