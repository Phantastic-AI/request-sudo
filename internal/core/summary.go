package core

import (
	"fmt"
	"strings"
)

func DescribeRequest(host string, requester PeerIdentity, argv []string, reason string) ApprovalSummary {
	requesterLabel := requester.Username
	if requesterLabel == "" {
		requesterLabel = fmt.Sprintf("uid:%d", requester.UID)
	}

	effect := "run a privileged command"
	risk := "privileged action requested; review exact argv before approval"
	if len(argv) > 0 {
		switch argv[0] {
		case "systemctl":
			effect = "change the lifecycle of a system service"
			risk = "may interrupt or restart a service if approved"
		case "apt", "apt-get", "dnf", "yum":
			effect = "change installed system packages"
			risk = "may alter package state or install new software"
		case "cp", "mv", "rm", "ln":
			effect = "change filesystem state"
			risk = "may overwrite, remove, or relink files"
		case "tee", "sed", "sh", "bash":
			effect = "mutate system configuration or execute shell logic"
			risk = "shell-level changes deserve close review before approval"
		}
	}

	if strings.TrimSpace(reason) == "" {
		reason = "No requester reason supplied."
	}

	return ApprovalSummary{
		Requester:    requesterLabel,
		Host:         host,
		ExactCommand: strings.Join(argv, " "),
		Reason:       reason,
		Effect:       effect,
		Risk:         risk,
	}
}
