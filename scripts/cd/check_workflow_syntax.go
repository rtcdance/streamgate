//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type runDefaults struct {
	Run struct {
		Shell string `yaml:"shell"`
	} `yaml:"run"`
}

type workflowStep struct {
	Name  string `yaml:"name"`
	Uses  string `yaml:"uses"`
	Run   string `yaml:"run"`
	Shell string `yaml:"shell"`
}

type workflowJob struct {
	Uses     string         `yaml:"uses"`
	Defaults runDefaults    `yaml:"defaults"`
	Steps    []workflowStep `yaml:"steps"`
}

type workflow struct {
	Defaults runDefaults            `yaml:"defaults"`
	Jobs     map[string]workflowJob `yaml:"jobs"`
}

var actionCommit = regexp.MustCompile(`@[0-9a-f]{40}$`)

func checkAction(uses string) error {
	if uses == "" || strings.HasPrefix(uses, "./") || actionCommit.MatchString(uses) {
		return nil
	}
	return fmt.Errorf("action or reusable workflow must use a full commit SHA: %s", uses)
}

func check(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var document workflow
	if err := yaml.Unmarshal(data, &document); err != nil {
		return err
	}
	if len(document.Jobs) == 0 {
		return fmt.Errorf("workflow has no jobs")
	}
	checked, otherShells := 0, 0
	for name, job := range document.Jobs {
		if err := checkAction(job.Uses); err != nil {
			return fmt.Errorf("job %s: %w", name, err)
		}
		for _, step := range job.Steps {
			if err := checkAction(step.Uses); err != nil {
				return fmt.Errorf("job %s step %s: %w", name, step.Name, err)
			}
			if step.Run == "" {
				continue
			}
			shell := step.Shell
			if shell == "" {
				shell = job.Defaults.Run.Shell
			}
			if shell == "" {
				shell = document.Defaults.Run.Shell
			}
			if shell == "" {
				shell = "bash"
			}
			fields := strings.Fields(shell)
			if len(fields) == 0 {
				return fmt.Errorf("job %s step %s: empty shell", name, step.Name)
			}
			commandName := fields[0]
			if commandName != "bash" && commandName != "sh" {
				otherShells++
				continue
			}
			command := exec.Command(commandName, "-n")
			command.Stdin = strings.NewReader(step.Run)
			if output, err := command.CombinedOutput(); err != nil {
				return fmt.Errorf("job %s step %s: %w: %s", name, step.Name, err, output)
			}
			checked++
		}
	}
	fmt.Printf("workflow YAML, action pins and shell syntax PASS: %s (%d shell steps; %d other-shell steps)\n", path, checked, otherShells)
	return nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run scripts/cd/check_workflow_syntax.go <workflow.yml> ...")
		os.Exit(2)
	}
	for _, path := range os.Args[1:] {
		if err := check(path); err != nil {
			fmt.Fprintf(os.Stderr, "workflow check FAILED: %s: %v\n", path, err)
			os.Exit(1)
		}
	}
}
