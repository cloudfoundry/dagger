package dagger

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/buildpack/libbuildpack"
)

const (
	originalImage = "cnb-pack-builder"
	builderImage  = "cnb-acceptance-builder"
)

type Dagger struct {
	rootDir, workspaceDir, cacheDir, buildpackDir, inputsDir, packDir string
	buildpack                                                         libbuildpack.Buildpack
}

func NewDagger(rootDir string) (*Dagger, error) {
	workspaceDir, err := ioutil.TempDir("/tmp", "workspace")
	if err != nil {
		return nil, err
	}

	if err := os.Chmod(workspaceDir, os.ModePerm); err != nil {
		return nil, err
	}

	cacheDir, err := ioutil.TempDir("/tmp", "cache")
	if err != nil {
		return nil, err
	}

	if err := os.Chmod(cacheDir, os.ModePerm); err != nil {
		return nil, err
	}

	inputsDir, err := ioutil.TempDir("/tmp", "inputs")
	if err != nil {
		return nil, err
	}

	if err := os.Chmod(inputsDir, os.ModePerm); err != nil {
		return nil, err
	}

	packDir, err := ioutil.TempDir("/tmp", "pack")
	if err != nil {
		return nil, err
	}

	buildpack := libbuildpack.Buildpack{}
	_, err = toml.DecodeFile(filepath.Join(rootDir, "buildpack.toml"), &buildpack)
	if err != nil {
		return nil, err
	}

	dagg := &Dagger{
		rootDir:      rootDir,
		workspaceDir: workspaceDir,
		cacheDir:     cacheDir,
		inputsDir:    inputsDir,
		packDir:      packDir,
		buildpack:    buildpack,
	}

	buildpackDir, err := dagg.bundleBuildpack()
	if err != nil {
		return nil, err
	}

	if err := os.Chmod(buildpackDir, 0755); err != nil {
		return nil, err
	}

	dagg.buildpackDir = buildpackDir

	return dagg, nil
}

func (d *Dagger) Destroy() {
	os.RemoveAll(d.workspaceDir)
	d.workspaceDir = ""

	os.RemoveAll(d.cacheDir)
	d.cacheDir = ""

	os.RemoveAll(d.buildpackDir)
	d.buildpackDir = ""

	os.RemoveAll(d.inputsDir)
	d.inputsDir = ""

	os.RemoveAll(d.packDir)
	d.packDir = ""
}

func (d *Dagger) bundleBuildpack() (string, error) {
	cmd := exec.Command("bash", "package.sh")
	cmd.Dir = filepath.Join(d.rootDir, "scripts")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	r := regexp.MustCompile("Buildpack packaged into: (.*)")
	bpDir := r.FindStringSubmatch(string(out))[1]
	return bpDir, nil
}

//Group should be in libbuildpack
type Group struct {
	Buildpacks []libbuildpack.BuildpackInfo
}

type DetectResult struct {
	Group     Group
	BuildPlan libbuildpack.BuildPlan
}

type Order struct {
	Groups []Group
}

func (d *Dagger) Detect(appDir string, order Order) (*DetectResult, error) {
	if err := d.writeInput(order, "order.toml"); err != nil {
		return nil, err
	}

	cmd := exec.Command(
		"docker",
		"run",
		"--rm",
		"-v",
		fmt.Sprintf("%s:/workspace", d.workspaceDir),
		"-v",
		fmt.Sprintf("%s:/workspace/app", appDir),
		"-v",
		fmt.Sprintf("%s:/buildpacks/%s/latest", d.buildpackDir, d.buildpack.Info.ID),
		"-v",
		fmt.Sprintf("%s:/buildpacks/%s/%s", d.buildpackDir, d.buildpack.Info.ID, d.buildpack.Info.Version),
		"-v",
		fmt.Sprintf("%s:/inputs", d.inputsDir),
		os.Getenv("CNB_BUILD_IMAGE"),
		"/lifecycle/detector",
		"-buildpacks",
		"/buildpacks",
		"-order",
		"/inputs/order.toml",
		"-group",
		"/workspace/group.toml",
		"-plan",
		"/workspace/plan.toml",
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	result := &DetectResult{}

	_, err := toml.DecodeFile(filepath.Join(d.workspaceDir, "group.toml"), &result.Group)
	if err != nil {
		return nil, err
	}

	_, err = toml.DecodeFile(filepath.Join(d.workspaceDir, "plan.toml"), &result.BuildPlan)
	if err != nil {
		return nil, err
	}

	return result, nil
}

type Metadata struct {
	Version string
}

type BuildResult struct {
	LaunchRootDir string
	CacheRootDir  string
}

func (b *BuildResult) GetLayerMetadata(dep string) (Metadata, bool, error) {
	var metadata Metadata

	file := filepath.Join(b.LaunchRootDir, fmt.Sprintf("%s.toml", dep))
	if exists, err := FileExists(file); err != nil {
		return metadata, false, err
	} else if !exists {
		return metadata, false, nil
	}

	_, err := toml.DecodeFile(file, &metadata)
	if err != nil {
		return metadata, false, err
	}

	return metadata, true, nil
}

func (b *BuildResult) GetCacheLayerEnv(dep string) (map[string]string, error) {
	envMap := make(map[string]string)

	envFiles, err := filepath.Glob(filepath.Join(b.CacheRootDir, dep, "env", "*"))
	if err != nil {
		return envMap, err
	}

	for _, path := range envFiles {
		value, err := ioutil.ReadFile(path)
		if err != nil {
			return envMap, err
		}
		key := strings.TrimSuffix(filepath.Base(path), ".override")
		envMap[key] = string(value)
	}

	return envMap, nil
}

func (b *BuildResult) GetLaunchLayerEnv(dep string) (map[string]string, error) {
	envMap := make(map[string]string)

	envFiles, err := filepath.Glob(filepath.Join(b.LaunchRootDir, dep, "profile.d", "*"))
	if err != nil {
		return envMap, err
	}

	for _, path := range envFiles {
		data, err := ioutil.ReadFile(path)
		if err != nil {
			return envMap, err
		}
		regex := regexp.MustCompile(`export\s+(.*)=(.*)`)
		matches := regex.FindStringSubmatch(string(data))
		envMap[matches[1]] = matches[2]
	}

	return envMap, nil
}

func (b *BuildResult) GetLaunchMetadata() (libbuildpack.LaunchMetadata, bool, error) {
	var metadata libbuildpack.LaunchMetadata

	file := filepath.Join(b.LaunchRootDir, "launch.toml")
	if exists, err := FileExists(file); err != nil {
		return metadata, false, err
	} else if !exists {
		return metadata, false, nil
	}

	_, err := toml.DecodeFile(file, &metadata)
	if err != nil {
		return metadata, false, err
	}

	return metadata, true, nil
}

func (d *Dagger) Build(appDir string, group Group, plan libbuildpack.BuildPlan) (*BuildResult, error) {
	if err := d.writeInput(group, "group.toml"); err != nil {
		return nil, err
	}

	if err := d.writeInput(plan, "plan.toml"); err != nil {
		return nil, err
	}

	cmd := exec.Command(
		"docker",
		"run",
		"--rm",
		"-v",
		fmt.Sprintf("%s:/workspace", d.workspaceDir),
		"-v",
		fmt.Sprintf("%s:/workspace/app", appDir),
		"-v",
		fmt.Sprintf("%s:/cache", d.cacheDir),
		"-v",
		fmt.Sprintf("%s:/buildpacks/%s/latest", d.buildpackDir, d.buildpack.Info.ID),
		"-v",
		fmt.Sprintf("%s:/buildpacks/%s/%s", d.buildpackDir, d.buildpack.Info.ID, d.buildpack.Info.Version),
		"-v",
		fmt.Sprintf("%s:/inputs", d.inputsDir),
		os.Getenv("CNB_BUILD_IMAGE"),
		"/lifecycle/builder",
		"-buildpacks",
		"/buildpacks",
		"-group",
		"/inputs/group.toml",
		"-plan",
		"/inputs/plan.toml",
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return &BuildResult{
		LaunchRootDir: filepath.Join(d.workspaceDir, d.buildpack.Info.ID),
		CacheRootDir:  filepath.Join(d.cacheDir, d.buildpack.Info.ID),
	}, nil
}

type BuilderMetadata struct {
	Buildpacks []struct {
		ID  string
		URI string
	}
	Groups []Group
}

func (b BuilderMetadata) writeToFile() (string, error) {
	builderFile, err := ioutil.TempFile("", "")
	if err != nil {
		return "", err
	}

	out, err := ToTomlString(b)
	if err != nil {
		return "", err
	}

	return builderFile.Name(), ioutil.WriteFile(builderFile.Name(), []byte(out), 0777)
}

func (d *Dagger) Pack(appDir string, builderMetadata BuilderMetadata) (*App, error) {
	builderFile, err := builderMetadata.writeToFile()
	if err != nil {
		return nil, err
	}
	defer os.Remove(builderFile)

	// TODO : replace the following with pack create-builder when it is ready
	cmd := exec.Command("pack", "create-builder", originalImage, "-b", builderFile)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	cmd = exec.Command("docker", "run", "--user", "root", originalImage, "chmod", "0755", "/buildpacks")
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

	cmd = exec.Command("docker", "commit", strings.TrimSpace(buf.String()), builderImage)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	// TODO : remove above the above when pack create-builder works

	appImageName := RandomString(16)
	cmd = exec.Command("pack", "build", appImageName, "--run-image", os.Getenv("CNB_RUN_IMAGE"), "--builder", builderImage, "--no-pull")
	cmd.Dir = appDir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return &App{imageName: appImageName}, nil
}

func (d *Dagger) writeInput(obj interface{}, fileName string) error {
	objString, err := ToTomlString(obj)
	if err != nil {
		return err
	}

	return WriteToFile(bytes.NewBufferString(objString), filepath.Join(d.inputsDir, fileName), os.ModePerm)
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
