// Provides integration from go tool2json report to Report Portal
package integration

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/skudasov/rpgoclient"
	"go.uber.org/zap"
	"io"
	"log"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	INVESTIGATE_BUG_TYPE  = "ti001"
	PRODUCT_BUG_TYPE      = "pb001"
	AUTOMATION_BUG_TYPE   = "ab001"
	NOT_ISSUE_BUG_TYPE    = "nd001"
	SYSTEM_ISSUE_BUG_TYPE = "si001"
)

var (
	//issueRe           = regexp.MustCompile("issue_link:(.+),.*issue_type:(.+)")
	testStatusRe    = regexp.MustCompile(`--- (.*):`)
	testSkipIssueRe = regexp.MustCompile(`https://insolar\.atlassian\.net/browse/([A-Z]+-\d+)`)
	testCaseIdRe    = regexp.MustCompile(`C(\d{1,8})\s(.*)`)
)

type Clientable interface {
	StartLaunch(name string, description string, startTimeStringRFC3339 string, tags []string, mode string) (string, error)
	FinishLaunch(status string, endTimeStringRFC3339 string) (string, error)
	StartTestItem(name string, itemType string, startTimeStringRFC3339 string, description string, tags []string, parameters []map[string]string) (string, error)
	StartTestItemId(parent string, name string, itemType string, startTimeStringRFC3339 string, description string, tags []string, parameters []map[string]string) (string, error)
	FinishTestItem(status string, endTimeStringRFC3339 string, issue map[string]interface{}) (string, error)
	FinishTestItemId(parent string, status string, endTimeStringRFC3339 string, issue map[string]interface{}) (string, error)
	LinkIssue(itemId int, ticketId string, link string) (string, error)
	Log(message string, level string) (string, error)
	LogId(id string, message string, level string) (string, error)
	LogBatch(messages []rpgoclient.LogPayload) error
	GetBaseUrl() string
	GetLaunchId() string
	GetProject() string
	GetToken() string
}

type RPAgent struct {
	c                   Clientable
	BtsProject          string
	BtsUrl              string
	Events              []*TestEvent
	JsonReportErrorsNum int
	Force               bool
	MaxRps              int
	TestLogBatch        []rpgoclient.LogPayload
	l                   *zap.SugaredLogger
}

// Creates new Report Portal agent
func NewRPAgent(baseUrl string, project string, token string, btsProject string, btsUrl string, dumptransport bool, options ...func(*RPAgent) error) *RPAgent {
	rpa := &RPAgent{
		Events:              make([]*TestEvent, 0),
		BtsProject:          btsProject,
		BtsUrl:              btsUrl,
		JsonReportErrorsNum: 0,
		Force:               false,
		MaxRps:              400,
		TestLogBatch:        make([]rpgoclient.LogPayload, 0),
		l:                   NewLogger("info"),
	}
	if rpa.c == nil {
		rpa.c = rpgoclient.New(baseUrl, project, token, btsProject, btsUrl, dumptransport)
	}
	for _, op := range options {
		err := op(rpa)
		if err != nil {
			log.Fatalf("option failed: %s", err)
		}
	}
	return rpa
}

func (m *RPAgent) SetLogger(l *zap.SugaredLogger) {
	m.l = l
}

func (m *RPAgent) SetForce(force bool) {
	m.Force = force
}

func WithRpClient(client Clientable) func(c *RPAgent) error {
	return func(rpa *RPAgent) error {
		rpa.c = client
		return nil
	}
}

func WithVerbosity(verbosity string) func(client *RPAgent) error {
	return func(c *RPAgent) error {
		c.l = NewLogger(verbosity)
		return nil
	}
}

func WithForce(force bool) func(client *RPAgent) error {
	return func(c *RPAgent) error {
		c.Force = force
		return nil
	}
}

func WithMaxRps(maxRps int) func(client *RPAgent) error {
	return func(c *RPAgent) error {
		c.MaxRps = maxRps
		return nil
	}
}

func (m *RPAgent) RunUrl(projectName string) string {
	return path.Join(m.c.GetBaseUrl(), "ui", "#"+projectName, "launches", "all", m.c.GetLaunchId())
}

// TestObject represents test data with aggregated log batch
type TestObject struct {
	FullPath    string
	Package     string
	Status      string
	CaseID      int
	Desc        string
	GoTestName  string
	IssueURL    string
	IssueTicket string
	RawEvents   []*TestEvent
	OutputBatch []string
}

func UniqTestKey(e *TestEvent) string {
	return e.Test + "|" + e.Package
}

func parseEventsBatch(stream io.Reader) []*TestEvent {
	testEvents := make([]*TestEvent, 0)
	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		var te *TestEvent
		if err := json.Unmarshal([]byte(scanner.Text()), &te); err != nil {
			log.Fatalf("failed to unmarshal test event json: %s\n", err)
		}
		testEvents = append(testEvents, te)
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
	return testEvents
}

// groupEventsByTest groups test2json events by test + package key
func groupEventsByTest(events []*TestEvent) map[string][]*TestEvent {
	eventsByTest := make(map[string][]*TestEvent)
	testNames := make(map[string]int)
	for _, e := range events {
		if _, ok := testNames[UniqTestKey(e)]; !ok {
			testNames[UniqTestKey(e)] = 1
		}
	}
	for uniqTest := range testNames {
		for _, e := range events {
			if UniqTestKey(e) == uniqTest {
				eventsByTest[uniqTest] = append(eventsByTest[uniqTest], e)
			}
		}
	}
	return eventsByTest
}

// EventsToTestObjects parses event batches to construct TestObjects, extracting caseID, Description, Status and IssueURL
func EventsToTestObjects(events map[string][]*TestEvent) []*TestObject {
	tests := make([]*TestObject, 0)
	for _, eventsBatch := range events {
		t := &TestObject{}
		for _, e := range eventsBatch {
			t.Package = e.Package
			t.GoTestName = e.Test
			t.FullPath = e.Package + "|" + strings.Join(strings.Split(e.Test, "/"), "|")
			if e.Action == "pass" || e.Action == "skip" || e.Action == "fail" {
				t.Status = strings.ToUpper(e.Action)
			}
			if e.Action == "output" {
				// find TestRail format case id (C**** test description)
				res := testCaseIdRe.FindAllStringSubmatch(e.Output, -1)
				if len(res) != 0 && len(res[0]) == 3 {
					d, err := strconv.Atoi(res[0][1])
					if err != nil {
						log.Fatal(err)
					}
					if t.CaseID != 0 {
						continue
					}
					t.CaseID = d
					t.Desc = res[0][2]
				}
				// find issue link logged in test
				res = testSkipIssueRe.FindAllStringSubmatch(e.Output, -1)
				if len(res) != 0 && len(res[0]) == 2 {
					t.IssueURL = res[0][0]
					t.IssueTicket = res[0][1]
				}
				t.OutputBatch = append(t.OutputBatch, e.Output)
			}
			t.RawEvents = append(t.RawEvents, e)
		}
		tests = append(tests, t)
	}
	return tests
}

// Getting earliest and latest times to use it in StartItem or FinishItem calls
func (m *RPAgent) getTimeBounds(events []*TestEvent) (time.Time, time.Time) {
	b := make([]*TestEvent, len(events))
	copy(b, events)
	sort.Slice(b, func(i, j int) bool {
		return b[i].Time.Before(b[j].Time)
	})
	return b[0].Time, b[len(b)-1].Time
}

func (m *RPAgent) testrailTestcaseDesc(to *TestObject) string {
	return strconv.Itoa(to.CaseID) + " " + to.Desc
}

func eventsToObjects(events []*TestEvent) []*TestObject {
	groupedTestEventsBatch := groupEventsByTest(events)
	//f, err := os.Open("events.log")
	//if err != nil {
	//	log.Fatal(err)
	//}
	return EventsToTestObjects(groupedTestEventsBatch)
}

func (m *RPAgent) Report(jsonFilename string, runName string, projectName string, tag string) error {
	f, err := os.Open(jsonFilename)
	if err != nil {
		log.Fatal(err)
	}
	events := parseEventsBatch(f)
	testObjects := eventsToObjects(events)
	m.l.Infof(InfoColor, fmt.Sprintf("sending report to: %s, project: %s", m.c.GetBaseUrl(), m.c.GetProject()))

	alreadyStartedTestEntities := make(map[string]*RPTestEntity)
	mustFinishTestEntities := make(map[string]*RPTestEntity)

	earliestInReport, latestInReport := m.getTimeBounds(events)
	m.l.Debug("report time bounds: %s -> %s", earliestInReport, latestInReport)
	tags := strings.Split(tag, ",")
	_, err = m.c.StartLaunch(runName, runName, earliestInReport.Format(time.RFC3339), tags, "DEFAULT")
	if err != nil {
		m.l.Fatalf("error creating launch: %s", err)
	}

	for _, to := range testObjects {
		parent := ""
		itemType := "TEST"
		pathArray := strings.Split(to.FullPath, "|")
		for tIdx, tpath := range pathArray {
			if tpath == "" {
				continue
			}
			// start test items, starting from parents to child, add to alreadyStartedTestEntities
			if _, ok := alreadyStartedTestEntities[tpath]; !ok {
				//earliest, latest := m.getTimeBounds(to.RawEvents)
				if tIdx > 0 {
					parent = alreadyStartedTestEntities[pathArray[tIdx-1]].TestItemId
					itemType = "STEP"
				}
				m.l.Debugf("starting test: %s, parent: %s\n", tpath, parent)
				id, err := m.c.StartTestItemId(parent, tpath, itemType, time.Now().Format(time.RFC3339), tpath, nil, nil)
				if err != nil {
					log.Fatal(err)
				}
				m.l.Debugf("test started: name: %s, id: %s\n", tpath, id)
				endTime := time.Now().Format(time.RFC3339)
				alreadyStartedTestEntities[tpath] = &RPTestEntity{id, to.IssueTicket, to.IssueURL, endTime, to.Status, 0}
				mustFinishTestEntities[to.FullPath+tpath] = &RPTestEntity{id, to.IssueTicket, to.IssueURL, endTime, to.Status, 0}

				m.l.Debugf("uploading logs to id: %s\n", id)
				_, err = m.c.LogId(id, strings.Join(to.OutputBatch, ""), "INFO")
				if err != nil {
					log.Fatal(err)
				}
			}
		}
	}
	for _, startedObj := range mustFinishTestEntities {
		stat := eventActionStatusToRPStatus(startedObj.Status)
		if stat == "" {
			stat = "FAILED"
		}
		var issue map[string]interface{}
		if startedObj.IssueURL != "" {
			issue = m.linkIssue(startedObj)
		}
		if _, err := m.c.FinishTestItemId(startedObj.TestItemId, stat, time.Now().Format(time.RFC3339), issue); err != nil {
			log.Fatal(err)
		}
	}
	// status is calculated automatically
	if _, err := m.c.FinishLaunch("FAILED", latestInReport.Format(time.RFC3339)); err != nil {
		log.Fatal(err)
	}
	m.l.Infof(InfoColor, fmt.Sprintf("report launch url: %s", m.RunUrl(projectName)))
	return nil
}

func (m *RPAgent) linkIssue(startedObj *RPTestEntity) map[string]interface{} {
	issue := make(map[string]interface{})
	issue["issueType"] = PRODUCT_BUG_TYPE
	issue["comment"] = startedObj.IssueURL
	issue["externalSystemIssues"] = []map[string]string{
		{
			"btsProject": m.BtsProject,
			"btsUrl":     m.BtsUrl,
			"ticketId":   startedObj.IssueTicket,
			"url":        startedObj.IssueURL,
		},
	}
	m.l.Infof("item with issue: %s", startedObj.TestItemId)
	//if _, err := m.c.LinkIssue(14444, startedObj.IssueTicket, startedObj.IssueURL); err != nil {
	//	log.Fatal(err)
	//}
	return issue
}

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
