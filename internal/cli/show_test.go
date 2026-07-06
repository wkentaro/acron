package cli

import (
	"testing"

	"github.com/wkentaro/acron/internal/scheduler"
)

func TestRenderUnitDriftShowsFullUnitDelta(t *testing.T) {
	unit := scheduler.UnitFile{Name: "acron-x.timer", Installed: "a\nb\n", Desired: "a\nB\n"}

	out := renderUnit(unit)

	want := renderUnitFull(unit.Name, unit.Installed, unit.Desired)
	if out != want {
		t.Errorf("renderUnit =\n%q\nwant\n%q", out, want)
	}
}

func TestRenderUnitDesiredOnlyPrintsDesiredPlainly(t *testing.T) {
	unit := scheduler.UnitFile{Name: "acron-x.timer", Desired: "[Timer]\nOnCalendar=*-*-* 02:00:00\n"}

	if out := renderUnit(unit); out != unit.Desired {
		t.Errorf("renderUnit =\n%q\nwant\n%q", out, unit.Desired)
	}
}

func TestRenderUnitEqualSidesSkipsFullDiff(t *testing.T) {
	unit := scheduler.UnitFile{Name: "acron-x.timer", Installed: "a\nb\n", Desired: "a\nb\n"}

	if out := renderUnit(unit); out != unit.Desired {
		t.Errorf("renderUnit =\n%q\nwant\n%q", out, unit.Desired)
	}
}

func TestRenderUnitInstalledOnlyPrintsInstalledPlainly(t *testing.T) {
	unit := scheduler.UnitFile{Name: "acron-x.timer", Installed: "[Timer]\nOnCalendar=*-*-* 02:00:00\n"}

	if out := renderUnit(unit); out != unit.Installed {
		t.Errorf("renderUnit =\n%q\nwant\n%q", out, unit.Installed)
	}
}

func TestRenderUnitBothEmptyReturnsEmpty(t *testing.T) {
	if out := renderUnit(scheduler.UnitFile{Name: "acron-x.timer"}); out != "" {
		t.Errorf("renderUnit = %q, want empty", out)
	}
}
