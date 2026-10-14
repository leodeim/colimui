package app

import (
	"os/exec"
	"strings"
)

// logRequest selects the history a log stream starts with: a short tail
// unless all is set, keeping only lines after the docker timestamp since.
type logRequest struct {
	follow bool
	all    bool
	since  string
}

type Backend interface {
	Profiles() ([]profile, error)
	Containers(profileName string) ([]container, error)
	Action(profileName, command string, args ...string) error
	OpenLogs(profileName, id string, req logRequest) (*logReader, error)
	Shell(profileName, id string) *exec.Cmd
}

func dockerContext(profileName string) string {
	if profileName == "" || profileName == "default" {
		return "colima"
	}
	return "colima-" + profileName
}

func isRunning(status string) bool {
	return strings.EqualFold(status, "running")
}
