package dagger

import (
	"bytes"
	"fmt"
	"github.com/BurntSushi/toml"
	"io"
	"math/rand"
	"os"
	"path/filepath"
)

func FindRoot() (string, error) {
	dir, err := filepath.Abs(".")
	if err != nil {
		return "", err
	}

	for {
		if dir == "/" {
			return "", fmt.Errorf("could not find go.mod file in the directory hierarchy")
		}

		if exist, err := FileExists(filepath.Join(dir, "go.mod")); err != nil {
			return "", err
		} else if exist {
			return dir, nil
		}

		dir, err = filepath.Abs(filepath.Join(dir, ".."))
		if err != nil {
			return "", err
		}
	}
}

func FileExists(filePath string) (bool, error) {
	_, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func CopyFile(from, to string) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(to)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}

func RandomString(n int) string {
	letterRunes := []rune("abcdefghijklmnopqrstuvwxyz")
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func ToTomlString(v interface{}) (string, error) {
	var b bytes.Buffer

	if err := toml.NewEncoder(&b).Encode(v); err != nil {
		return "", err
	}

	return b.String(), nil
}

func WriteToFile(source io.Reader, destFile string, mode os.FileMode) error {
	err := os.MkdirAll(filepath.Dir(destFile), 0755)
	if err != nil {
		return err
	}

	fh, err := os.OpenFile(destFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer fh.Close()

	_, err = io.Copy(fh, source)
	if err != nil {
		return err
	}

	return nil
}
