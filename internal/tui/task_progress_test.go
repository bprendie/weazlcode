package tui

import (
	"strings"
	"testing"

	"github.com/bprendie/weazlcode/internal/coding"
)

func TestTaskProgressSummary(t *testing.T) {
	plan := coding.Plan{Tasks: []coding.Task{
		{Status: coding.TaskStatusDone},
		{Status: coding.TaskStatusRunning},
		{Status: coding.TaskStatusReviewing},
		{Status: coding.TaskStatusBlocked},
		{Status: coding.TaskStatusPending},
	}}
	got := taskProgressSummary(plan)
	for _, want := range []string{"1/5 done", "1 running", "1 reviewing", "1 blocked", "1 pending"} {
		if !strings.Contains(got, want) {
			t.Fatalf("taskProgressSummary missing %q: %q", want, got)
		}
	}
}

func TestTaskProgressBadgeComplete(t *testing.T) {
	plan := coding.Plan{Tasks: []coding.Task{
		{Status: coding.TaskStatusDone},
		{Status: coding.TaskStatusDone},
	}}
	got := taskProgressBadge(plan)
	if got != "task 2/2 complete" {
		t.Fatalf("taskProgressBadge = %q", got)
	}
}

func TestTaskProgressBadgeWithRunningSpinner(t *testing.T) {
	plan := coding.Plan{Tasks: []coding.Task{
		{Status: coding.TaskStatusRunning},
		{Status: coding.TaskStatusPending},
	}}
	got := taskProgressBadgeWithSpinner(plan, "*")
	if !strings.Contains(got, "* 1 running") {
		t.Fatalf("taskProgressBadgeWithSpinner = %q", got)
	}
}

func TestTaskProgressBadgeWithReviewingSpinner(t *testing.T) {
	plan := coding.Plan{Tasks: []coding.Task{
		{Status: coding.TaskStatusReviewing},
		{Status: coding.TaskStatusPending},
	}}
	got := taskProgressBadgeWithSpinner(plan, "*")
	if !strings.Contains(got, "* 1 reviewing") {
		t.Fatalf("taskProgressBadgeWithSpinner = %q", got)
	}
}
