package runner

import (
	"fmt"
	"slices"
	"testing"
)

func retainHistoryLogs(records []Record) []string {
	kept := retainHistory(records)
	tags := make([]string, len(kept))
	for i, rec := range kept {
		tags[i] = rec.Log
	}
	return tags
}

func TestRetainHistoryNilReturnsEmpty(t *testing.T) {
	if got := retainHistory(nil); len(got) != 0 {
		t.Errorf("retainHistory(nil) kept %d records, want 0", len(got))
	}
}

func TestRetainHistoryUnderLimitKeepsAllInOrder(t *testing.T) {
	var records []Record
	var want []string
	for i := 0; i < keepRuns-1; i++ {
		tag := fmt.Sprintf("r%d", i)
		records = append(records, Record{Status: StatusSuccess, Log: tag})
		want = append(want, tag)
	}
	if got := retainHistoryLogs(records); !slices.Equal(got, want) {
		t.Errorf("kept %v, want %v", got, want)
	}
}

func TestRetainHistoryAtLimitKeepsAll(t *testing.T) {
	var records []Record
	var want []string
	for i := 0; i < keepRuns; i++ {
		tag := fmt.Sprintf("r%d", i)
		records = append(records, Record{Status: StatusSuccess, Log: tag})
		want = append(want, tag)
	}
	if got := retainHistoryLogs(records); !slices.Equal(got, want) {
		t.Errorf("at exactly keepRuns, kept %v, want all %v (nothing should drop)", got, want)
	}
}

func TestRetainHistoryKeepsLastRealRunsInOrder(t *testing.T) {
	realStatuses := []Status{StatusSuccess, StatusFailure, StatusTimeout, StatusInterrupted}
	const extra = 5
	var records []Record
	var want []string
	for i := 0; i < keepRuns+extra; i++ {
		tag := fmt.Sprintf("r%d", i)
		records = append(records, Record{Status: realStatuses[i%len(realStatuses)], Log: tag})
		if i >= extra {
			want = append(want, tag)
		}
	}
	if got := retainHistoryLogs(records); !slices.Equal(got, want) {
		t.Errorf("kept %v, want last %d reals %v", got, keepRuns, want)
	}
}

func TestRetainHistoryCapsRealsAndSkipsIndependentlyPreservingOrder(t *testing.T) {
	const extraReal, extraSkip = 2, 5
	var records []Record
	var want []string
	for i := 0; i < keepRuns+extraSkip; i++ {
		if i < keepRuns+extraReal {
			realTag := fmt.Sprintf("r%d", i)
			records = append(records, Record{Status: StatusSuccess, Log: realTag})
			if i >= extraReal {
				want = append(want, realTag)
			}
		}
		skipTag := fmt.Sprintf("s%d", i)
		records = append(records, Record{Status: StatusSkipped, Reason: ReasonCondition, Log: skipTag})
		if i >= extraSkip {
			want = append(want, skipTag)
		}
	}
	if got := retainHistoryLogs(records); !slices.Equal(got, want) {
		t.Errorf("kept %v, want last %d of each kind interleaved %v", got, keepRuns, want)
	}
}
