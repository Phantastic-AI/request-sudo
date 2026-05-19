package twilioadapter

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ApproversPolicy is a minimal parse of approvers.yaml — only the fields
// the adapter needs for routing per ADR-0005 T13–T16. We deliberately do
// NOT implement the full schema here; ADR-0002 owns that. This is a
// loose YAML reader scoped to the overnight test plan.
type ApproversPolicy struct {
	// ApproverSets maps set-name → list of approver names.
	ApproverSets map[string][]string
	// Phones maps approver-name → list of E.164 phone numbers.
	Phones map[string][]string
	// Routing maps requester-name → approver-set-name.
	Routing map[string]string
}

// LookupRecipients returns the union of phones (deduplicated, order
// preserved by first-seen) for the requester's approver set. T13–T16.
// If the requester has no routing entry or no phones, returns an empty
// slice. T16: caller treats empty as "fall back to local-only".
func (p ApproversPolicy) LookupRecipients(requester string) []string {
	setName, ok := p.Routing[requester]
	if !ok {
		return nil
	}
	members, ok := p.ApproverSets[setName]
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, member := range members {
		for _, phone := range p.Phones[member] {
			phone = strings.TrimSpace(phone)
			if phone == "" || seen[phone] {
				continue
			}
			seen[phone] = true
			out = append(out, phone)
		}
	}
	return out
}

// LoadApprovers parses a minimal subset of approvers.yaml. Intentional
// limitations: no anchors, no aliases, no nested-flow YAML; only the
// two-space indentation pattern used in the v1 sandbox approvers.yaml.
// Anything beyond that should be rejected loudly so we don't silently
// drift from the ADR-0002 schema.
func LoadApprovers(path string) (ApproversPolicy, error) {
	var policy ApproversPolicy
	abs, err := filepath.Abs(path)
	if err != nil {
		return policy, err
	}
	file, err := os.Open(abs)
	if err != nil {
		return policy, fmt.Errorf("open %s: %w", abs, err)
	}
	defer file.Close()
	return parseApprovers(file)
}

type yamlSection int

const (
	secNone yamlSection = iota
	secApproverSets
	secApprovers
	secRouting
)

func parseApprovers(r io.Reader) (ApproversPolicy, error) {
	policy := ApproversPolicy{
		ApproverSets: map[string][]string{},
		Phones:       map[string][]string{},
		Routing:      map[string]string{},
	}
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var (
		section          yamlSection
		currentSetName   string
		currentApprover  string
		currentRequester string
		inPhones         bool
	)

	lineno := 0
	for scanner.Scan() {
		lineno++
		raw := scanner.Text()
		stripped := stripComment(raw)
		if strings.TrimSpace(stripped) == "" {
			continue
		}
		indent := leadingSpaces(stripped)
		trimmed := strings.TrimSpace(stripped)

		// Section header (zero indent, ends with ':').
		if indent == 0 && strings.HasSuffix(trimmed, ":") {
			head := strings.TrimSuffix(trimmed, ":")
			switch head {
			case "approver_sets":
				section = secApproverSets
			case "approvers":
				section = secApprovers
			case "routing":
				section = secRouting
			default:
				section = secNone
			}
			currentSetName = ""
			currentApprover = ""
			currentRequester = ""
			inPhones = false
			continue
		}

		// Top-level scalars (e.g. wall_notify: true). Skip — adapter
		// doesn't care.
		if indent == 0 && strings.Contains(trimmed, ":") {
			section = secNone
			continue
		}

		switch section {
		case secApproverSets:
			if indent == 2 && strings.HasSuffix(trimmed, ":") {
				currentSetName = strings.TrimSuffix(trimmed, ":")
				policy.ApproverSets[currentSetName] = nil
				continue
			}
			if indent >= 4 && strings.HasPrefix(trimmed, "- ") && currentSetName != "" {
				name := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
				name = strings.Trim(name, "\"'")
				policy.ApproverSets[currentSetName] = append(policy.ApproverSets[currentSetName], name)
				continue
			}
		case secApprovers:
			if indent == 2 && strings.HasSuffix(trimmed, ":") {
				currentApprover = strings.TrimSuffix(trimmed, ":")
				policy.Phones[currentApprover] = nil
				inPhones = false
				continue
			}
			if indent == 4 && trimmed == "phones:" {
				inPhones = true
				continue
			}
			if inPhones && indent >= 6 && strings.HasPrefix(trimmed, "- ") && currentApprover != "" {
				phone := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
				phone = strings.Trim(phone, "\"'")
				policy.Phones[currentApprover] = append(policy.Phones[currentApprover], phone)
				continue
			}
			// any other line at indent 4 closes the phones block
			if indent == 4 {
				inPhones = false
			}
		case secRouting:
			if indent == 2 && strings.HasSuffix(trimmed, ":") {
				currentRequester = strings.TrimSuffix(trimmed, ":")
				continue
			}
			if indent >= 4 && currentRequester != "" {
				// key: value
				colon := strings.IndexByte(trimmed, ':')
				if colon < 0 {
					continue
				}
				key := strings.TrimSpace(trimmed[:colon])
				value := strings.TrimSpace(trimmed[colon+1:])
				value = strings.Trim(value, "\"'")
				if key == "approver_set" {
					policy.Routing[currentRequester] = value
				}
				continue
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return policy, err
	}
	if len(policy.ApproverSets) == 0 && len(policy.Routing) == 0 {
		return policy, errors.New("approvers.yaml empty or unparseable (no approver_sets or routing)")
	}
	return policy, nil
}

func stripComment(line string) string {
	// Naive — does not handle '#' inside quoted strings. Adequate for
	// our YAML subset.
	if idx := strings.IndexByte(line, '#'); idx >= 0 {
		return line[:idx]
	}
	return line
}

func leadingSpaces(s string) int {
	n := 0
	for _, r := range s {
		if r == ' ' {
			n++
			continue
		}
		break
	}
	return n
}
