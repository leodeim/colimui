package main

import "strings"

type Backend interface {
	Profiles() ([]profile, error)
	Containers(profileName string) ([]container, error)
	Action(profileName, command string, args ...string) error
	OpenLogs(profileName, id string, follow, fromStart bool) (*logReader, error)
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
