package main

import (
	"strconv"
	"strings"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	sessions "github.com/gastownhall/gascity/internal/session"
)

type wakeProvenance struct {
	Reason           string
	Source           string
	Template         string
	PoolDesiredCount int
	WorkBeadID       string
}

func wakeProvenanceForStart(
	session beads.Bead,
	tp TemplateParams,
	reason string,
	poolDesired map[string]int,
	assignedWorkBeads []beads.Bead,
	cfg *config.City,
) wakeProvenance {
	template := tp.TemplateName
	if template == "" {
		template = normalizedSessionTemplate(session, cfg)
	}
	provenance := wakeProvenance{
		Reason:   strings.TrimSpace(reason),
		Source:   wakeSourceForReason(reason),
		Template: template,
	}
	if template != "" {
		provenance.PoolDesiredCount = poolDesired[template]
	}
	if reason == "assigned-work" {
		provenance.WorkBeadID = assignedWakeWorkBeadID(session, assignedWorkBeads)
	}
	return provenance
}

func wakeSourceForReason(reason string) string {
	switch {
	case strings.HasPrefix(reason, "scaled:"):
		return "scale_check"
	case reason == "assigned-work":
		return "assigned_work"
	case reason == "work-query":
		return "work_query"
	case reason == "pending-create":
		return "pending_create"
	case reason == "wait-ready":
		return "ready_wait"
	case reason == "attached":
		return "attached"
	case reason == "pending":
		return "runtime_pending"
	case reason == "pin":
		return "pin"
	case strings.HasPrefix(reason, "named-"):
		return "named_session"
	case reason == "manual":
		return "manual"
	case reason == "on-demand:running":
		return "on_demand_running"
	default:
		return strings.TrimSpace(reason)
	}
}

func assignedWakeWorkBeadID(session beads.Bead, work []beads.Bead) string {
	name := strings.TrimSpace(session.Metadata["session_name"])
	namedIdentity := strings.TrimSpace(session.Metadata["configured_named_identity"])
	for _, wb := range work {
		if wb.Status != "open" && wb.Status != "in_progress" {
			continue
		}
		assignee := strings.TrimSpace(wb.Assignee)
		if assignee == "" {
			continue
		}
		if assignee == session.ID || assignee == name || (namedIdentity != "" && assignee == namedIdentity) {
			return wb.ID
		}
	}
	return ""
}

func applyWakeProvenancePatch(patch sessions.MetadataPatch, provenance wakeProvenance) {
	patch["last_wake_reason"] = strings.TrimSpace(provenance.Reason)
	patch["last_wake_source"] = strings.TrimSpace(provenance.Source)
	patch["last_wake_template"] = strings.TrimSpace(provenance.Template)
	patch["last_wake_work_bead_id"] = strings.TrimSpace(provenance.WorkBeadID)
	if provenance.PoolDesiredCount > 0 {
		patch["last_wake_pool_desired_count"] = strconv.Itoa(provenance.PoolDesiredCount)
	} else {
		patch["last_wake_pool_desired_count"] = ""
	}
}

func annotateEmptyWakePatch(patch map[string]string, meta map[string]string, now time.Time) {
	patch["last_empty_wake_at"] = now.UTC().Format(time.RFC3339)
	patch["last_empty_wake_reason"] = strings.TrimSpace(meta["last_wake_reason"])
	patch["last_empty_wake_source"] = strings.TrimSpace(meta["last_wake_source"])
	patch["last_empty_wake_template"] = strings.TrimSpace(meta["last_wake_template"])
	patch["last_empty_wake_work_bead_id"] = strings.TrimSpace(meta["last_wake_work_bead_id"])
	patch["last_empty_wake_pool_desired_count"] = strings.TrimSpace(meta["last_wake_pool_desired_count"])
}
