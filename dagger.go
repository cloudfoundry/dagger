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

	"github.com/cloudfoundry/libcfbuildpack/helper"
)

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
	for _, buildpack := range buildpacks {
		cmd.Args = append(cmd.Args, "--buildpack", buildpack)
	}
	cmd.Dir = appDir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return &App{imageName: appImageName}, nil
}

type App struct {
	imageName   string
	containerId string
	port        string
}

func (a *App) Start() error {
	buf := &bytes.Buffer{}

	cmd := exec.Command("docker", "run", "-d", "-P", a.imageName)
	cmd.Stdout = buf
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	a.containerId = buf.String()[:12]

	// TODO : implement a timer that checks health and bails out after X tries
	// but for now lets just sleep :)
	// cmd = exec.Command("docker", "inspect", "-f", "{{.State.Health.Status}}", a.containerId)
	fmt.Fprintf(os.Stderr, "Waiting for container to become healthy...")
	time.Sleep(35 * time.Second)

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
