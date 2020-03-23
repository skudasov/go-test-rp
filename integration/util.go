package integration

import (
	"encoding/json"
	"log"
	"os"
	"sort"
	"strings"
	"time"
)

func eventActionStatusToRPStatus(event string) string {
	switch event {
	case "PASS":
		return "PASSED"
	case "FAIL":
		return "FAILED"
	case "SKIP":
		return "SKIPPED"
	}
	return ""
}

func parseTags(tag string) (tags []string) {
	if tag == "" {
		tags = nil
	} else {
		tags = strings.Split(tag, ",")
	}
	return
}

func sortTestObjectsByStartTime(to []*TestObject) {
	sort.SliceStable(to, func(i, j int) bool {
		return to[i].StartTime.Before(to[j].StartTime)
	})
}

func debugDumpEventsToFile(groupedTestEventsBatch map[string][]*TestEvent) {
	for ename, e := range groupedTestEventsBatch {
		f, err := os.Create(strings.ReplaceAll(ename, "/", "_") + ".tlog")
		if err != nil {
			log.Fatal(err)
		}
		for _, ev := range e {
			ev.Output = ""
			d, err := json.Marshal(ev)
			if err != nil {
				log.Fatal(err)
			}
			f.Write([]byte(string(d) + "\n"))
		}
		f.Close()
	}
}

// Getting earliest and latest times to use it in StartItem or FinishItem calls
func getTimeBounds(events []*TestEvent) (time.Time, time.Time) {
	return events[0].Time, events[len(events)-1].Time
}

func breadCrumbsFromFullPath(fullPath string) []string {
	fpath := strings.Split(fullPath, "|")
	bCrumbs := make([]string, 0)
	var nextPath string
	for _, fp := range fpath {
		if nextPath == "" {
			nextPath = fp
		} else {
			nextPath = strings.Join([]string{nextPath, fp}, "|")
		}
		bCrumbs = append(bCrumbs, nextPath)
	}
	return bCrumbs
}

func UniqTestKey(e *TestEvent) string {
	if e.Test != "" {
		var testName string
		if strings.Contains(e.Test, "/") {
			testName = strings.Join(strings.Split(e.Test, "/"), "|")
		} else {
			testName = e.Test
		}
		return e.Package + "|" + testName
	} else {
		return e.Package
	}
}
