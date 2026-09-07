package main

import "strings"

// logRequest selects how much history a log stream starts with: fromStart
// loads everything, since resumes after a timestamp, otherwise a short tail.
type logRequest struct {
	follow    bool
	fromStart bool
	since     string
}

type Backend interface {
	Profiles() ([]profile, error)
	Containers(profileName string) ([]container, error)
	Action(profileName, command string, args ...string) error
	OpenLogs(profileName, id string, req logRequest) (*logReader, error)
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
