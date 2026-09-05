package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

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
		cmd.Env = append(os.Environ(), "DOCKER_CONTEXT="+dockerContext(profileName))
	}
	output, err := cmd.CombinedOutput()
	if err != nil && len(output) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return err
}

func (execBackend) OpenLogs(profileName, id string, follow, fromStart bool) (*logReader, error) {
	ctx, cancel := context.WithCancel(context.Background())
	args := []string{"logs"}
	if !fromStart {
		args = append(args, "--tail", "200")
	}
	if follow {
		args = append(args, "--follow")
	}
	args = append(args, id)
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = append(os.Environ(), "DOCKER_CONTEXT="+dockerContext(profileName))
	reader, err := startLogReader(cmd, cancel)
	if err != nil {
		cancel()
		return nil, err
	}
	return reader, nil
}

func listProfiles() ([]profile, error) {
	output, err := exec.Command("colima", "list", "--json").Output()
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
	cmd := exec.Command("docker", "ps", "--all", "--format", "{{json .}}")
	cmd.Env = append(os.Environ(), "DOCKER_CONTEXT="+dockerContext(profileName))
	output, err := cmd.Output()
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
