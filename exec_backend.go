package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// listTimeout bounds each profile/container listing so a wedged daemon
// cannot stall a refresh or the menu bar poll loop.
const listTimeout = 15 * time.Second

type execBackend struct{}

func (execBackend) Profiles() ([]profile, error) {
	return listProfiles()
}

func (execBackend) Containers(profileName string) ([]container, error) {
	return listContainers(profileName)
}

func (execBackend) Action(profileName, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	if command == "docker" {
		cmd.Env = dockerEnv(profileName)
	}
	output, err := cmd.CombinedOutput()
	if err != nil && len(output) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return err
}

func (execBackend) OpenLogs(profileName, id string, req logRequest) (*logReader, error) {
	ctx, cancel := context.WithCancel(context.Background())
	args := []string{"logs", "--timestamps"}
	switch {
	case req.since != "":
		args = append(args, "--since", req.since)
	case !req.fromStart:
		args = append(args, "--tail", "200")
	}
	if req.follow {
		args = append(args, "--follow")
	}
	args = append(args, id)
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = dockerEnv(profileName)
	reader, err := startLogReader(cmd, cancel)
	if err != nil {
		cancel()
		return nil, err
	}
	return reader, nil
}

func (execBackend) Shell(profileName, id string) *exec.Cmd {
	cmd := exec.Command("docker", "exec", "-it", id, "sh", "-c", "command -v bash >/dev/null 2>&1 && exec bash || exec sh")
	cmd.Env = dockerEnv(profileName)
	return cmd
}

// commandOutput runs a bounded query; docker targets the profile's context.
// stderr is folded into the error because a bare exit status explains nothing.
func commandOutput(profileName string, timeout time.Duration, command string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	if command == "docker" {
		cmd.Env = dockerEnv(profileName)
	}
	output, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%s %s timed out after %s", command, strings.Join(args, " "), timeout)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if stderr := strings.TrimSpace(string(exit.Stderr)); stderr != "" {
			return nil, fmt.Errorf("%w: %s", err, stderr)
		}
	}
	return output, err
}

func dockerEnv(profileName string) []string {
	return append(os.Environ(), "DOCKER_CONTEXT="+dockerContext(profileName))
}

func listProfiles() ([]profile, error) {
	output, err := commandOutput("", listTimeout, "colima", "list", "--json")
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, err
	}
	var profiles []profile
	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &profiles); err != nil {
			return nil, err
		}
	} else {
		var p profile
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		profiles = []profile{p}
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles, nil
}

func listContainers(profileName string) ([]container, error) {
	output, err := commandOutput(profileName, listTimeout, "docker", "ps", "--all", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	var containers []container
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		var item struct {
			ID      string `json:"ID"`
			Names   string `json:"Names"`
			Image   string `json:"Image"`
			Command string `json:"Command"`
			State   string `json:"State"`
			Status  string `json:"Status"`
			Ports   string `json:"Ports"`
			Labels  string `json:"Labels"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			return nil, err
		}
		project, service := composeLabels(item.Labels)
		containers = append(containers, container{ID: item.ID, Name: item.Names, Image: item.Image, Command: item.Command, State: item.State, Status: item.Status, Ports: item.Ports, ComposeProject: project, ComposeService: service})
	}
	sort.SliceStable(containers, func(i, j int) bool {
		if containers[i].State == containers[j].State {
			return containers[i].Name < containers[j].Name
		}
		return containers[i].State == "running"
	})
	return containers, scanner.Err()
}

func composeLabels(labels string) (string, string) {
	var project, service string
	for _, label := range strings.Split(labels, ",") {
		key, value, ok := strings.Cut(label, "=")
		if !ok {
			continue
		}
		switch key {
		case "com.docker.compose.project":
			project = value
		case "com.docker.compose.service":
			service = value
		}
	}
	return project, service
}
