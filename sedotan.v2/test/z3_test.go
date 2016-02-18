package main

import (
	"os"
	"os/exec"
	"testing"
)

var (
	basePath string = func(dir string, err error) string { return dir }(os.Getwd())
	cmd      *exec.Cmd
)

func TestBuild(t *testing.T) {
	cmd = exec.Command("go", "build", "../sedotanwd")
	cmd.Run()
	cmd.Wait()

	cmd = exec.Command("rm", "-rf", "cli")
	cmd.Run()
	cmd.Wait()
}

func TestRunCommand(t *testing.T) {
	cmd = exec.Command("go", "run", "../sedotanwd/main.go", `-config="config.json"`, "-debug=true")
	cmd.Run()
	cmd.Wait()
}
