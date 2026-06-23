package cli

import (
	"strings"
	"testing"

	"github.com/wkentaro/acron/internal/scheduler"
)

func TestRenderPlanEmpty(t *testing.T) {
	if got := renderPlan(&scheduler.Plan{}, "Would apply:", true); got != "Nothing to do.\n" {
		t.Errorf("empty plan = %q, want \"Nothing to do.\\n\"", got)
	}
}

func TestRenderPlanTerseSummaryForRealApply(t *testing.T) {
	plan := &scheduler.Plan{
		Created: []string{"fresh"},
		Updated: []string{"existing"},
		Removed: []string{"ghost"},
	}

	out := renderPlan(plan, "Applied:", false)

	for _, want := range []string{"Applied:\n", "+ fresh\n", "~ existing\n", "- ghost\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("terse summary missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "@@") || strings.Contains(out, "--- ") {
		t.Errorf("a real apply must not render diffs\n---\n%s", out)
	}
}

func TestRenderPlanDryRunShowsDiffPerJob(t *testing.T) {
	plan := &scheduler.Plan{
		Created: []string{"fresh"},
		Updated: []string{"drifted"},
		Removed: []string{"ghost"},
		Changes: []scheduler.PlanChange{
			{Name: "fresh", Units: []scheduler.UnitFile{
				{Name: "acron-fresh.timer", Desired: "OnCalendar=new\n"},
			}},
			{Name: "drifted", Units: []scheduler.UnitFile{
				{Name: "acron-drifted.timer", Installed: "OnCalendar=old\n", Desired: "OnCalendar=new\n"},
			}},
			{Name: "ghost", Units: []scheduler.UnitFile{
				{Name: "acron-ghost.timer", Installed: "OnCalendar=old\n"},
			}},
		},
	}

	out := renderPlan(plan, "Would apply:", true)

	for _, want := range []string{
		"+ fresh\n",
		"--- /dev/null\n+++ b/acron-fresh.timer\n",
		"~ drifted\n",
		"--- a/acron-drifted.timer\n+++ b/acron-drifted.timer\n",
		"-OnCalendar=old\n",
		"+OnCalendar=new\n",
		"- ghost\n",
		"--- a/acron-ghost.timer\n+++ /dev/null\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run diff missing %q\n---\n%s", want, out)
		}
	}
}

func TestRenderPlanDryRunOmitsByteIdenticalUnit(t *testing.T) {
	plan := &scheduler.Plan{
		Updated: []string{"drifted"},
		Changes: []scheduler.PlanChange{{
			Name: "drifted",
			Units: []scheduler.UnitFile{
				{Name: "acron-drifted.service", Installed: "same\n", Desired: "same\n"},
				{Name: "acron-drifted.timer", Installed: "OnCalendar=old\n", Desired: "OnCalendar=new\n"},
			},
		}},
	}

	out := renderPlan(plan, "Would apply:", true)

	if strings.Contains(out, "acron-drifted.service") {
		t.Errorf("a byte-identical unit should not appear\n---\n%s", out)
	}
	if !strings.Contains(out, "acron-drifted.timer") {
		t.Errorf("the changed unit should appear\n---\n%s", out)
	}
}

func TestRenderPlanDryRunSeparatesJobSections(t *testing.T) {
	plan := &scheduler.Plan{
		Created: []string{"fresh"},
		Removed: []string{"ghost"},
		Changes: []scheduler.PlanChange{
			{Name: "fresh", Units: []scheduler.UnitFile{
				{Name: "acron-fresh.timer", Desired: "OnCalendar=new\n"},
			}},
			{Name: "ghost", Units: []scheduler.UnitFile{
				{Name: "acron-ghost.timer", Installed: "OnCalendar=old\n"},
			}},
		},
	}

	out := renderPlan(plan, "Would apply:", true)

	if !strings.Contains(out, "\n\n  - ghost\n") {
		t.Errorf("want a blank line separating consecutive job sections\n---\n%s", out)
	}
}

func TestRenderPlanDryRunSeparatesMultipleUnitDiffs(t *testing.T) {
	plan := &scheduler.Plan{
		Updated: []string{"drifted"},
		Changes: []scheduler.PlanChange{{
			Name: "drifted",
			Units: []scheduler.UnitFile{
				{Name: "acron-drifted.service", Installed: "ExecStart=old\n", Desired: "ExecStart=new\n"},
				{Name: "acron-drifted.timer", Installed: "OnCalendar=old\n", Desired: "OnCalendar=new\n"},
			},
		}},
	}

	out := renderPlan(plan, "Would apply:", true)

	if !strings.Contains(out, "\n\n--- a/acron-drifted.timer\n") {
		t.Errorf("want a blank line separating consecutive unit diffs\n---\n%s", out)
	}
}

func TestRenderPlanDryRunInactiveTimerStatesReasonWithoutDiff(t *testing.T) {
	plan := &scheduler.Plan{
		Updated: []string{"idle"},
		Changes: []scheduler.PlanChange{{
			Name:           "idle",
			UnitsUnchanged: true,
			Units: []scheduler.UnitFile{
				{Name: "acron-idle.timer", Installed: "OnCalendar=same\n", Desired: "OnCalendar=same\n"},
			},
		}},
	}

	out := renderPlan(plan, "Would apply:", true)

	if !strings.Contains(out, "~ idle (units unchanged, would reload and restart)\n") {
		t.Errorf("want the units-unchanged reason note\n---\n%s", out)
	}
	if strings.Contains(out, "@@") || strings.Contains(out, "--- ") {
		t.Errorf("an inactive-timer job must not render a diff body\n---\n%s", out)
	}
}
