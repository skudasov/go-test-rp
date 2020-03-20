package integration

import "time"

type TestEvent struct {
	Time    time.Time
	Action  string
	Package string
	Test    string
	Elapsed float64
	Output  string
}

type RPTestEntity struct {
	TestItemId  string
	IssueTicket string
	IssueURL    string
	EndTime     string
	Status      string
	FailedTests int
}

type TestRuntimeInfo struct {
	Parent string
	Issue  map[string]string
}
