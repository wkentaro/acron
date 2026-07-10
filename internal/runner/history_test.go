package runner

import (
	"os"
	"testing"

	"github.com/wkentaro/acron/internal/paths"
)

func TestHistorySkipsBlankAndMalformedLines(t *testing.T) {
	job := "corruptjob"
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if _, err := ensureRunsDir(job); err != nil {
		t.Fatal(err)
	}
	content := `{"status":"success","log":"first"}` + "\n" +
		"\n" +
		"not json at all" + "\n" +
		`{"status":"failure","log":"second"}` + "\n"
	if err := os.WriteFile(paths.HistoryPath(job), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	records, err := History(job)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("History kept %d records, want 2 (blank and malformed lines dropped)", len(records))
	}
	if records[0].Log != "first" || records[0].Status != StatusSuccess {
		t.Errorf("records[0] = %+v, want first/success", records[0])
	}
	if records[1].Log != "second" || records[1].Status != StatusFailure {
		t.Errorf("records[1] = %+v, want second/failure", records[1])
	}
}
