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
	StartLaunch(name string, description string, startTimeStringRFC3339 string, tags []string, mode string) (rpgoclient.StartLaunchResponse, error)
	FinishLaunch(status string, endTimeStringRFC3339 string) (rpgoclient.FinishLaunchResponse, error)
	StartTestItem(name string, itemType string, startTimeStringRFC3339 string, description string, tags []string, parameters []map[string]string) (rpgoclient.StartTestItemResponse, error)
	StartTestItemId(parent string, name string, itemType string, startTimeStringRFC3339 string, description string, tags []string, parameters []map[string]string) (rpgoclient.StartTestItemResponse, error)
	FinishTestItem(status string, endTimeStringRFC3339 string, issue map[string]interface{}) (string, error)
	FinishTestItemId(parent string, status string, endTimeStringRFC3339 string, issue map[string]interface{}) (string, error)
	LinkIssue(itemId int, ticketId string, link string) (string, error)
	Log(message string, level string) (string, error)
	LogId(id string, message string, level string) (string, error)
	LogBatch(messages []rpgoclient.LogPayload) error
	GetItemIdByUUID(uuid string) (rpgoclient.GetItemResponse, error)
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
	FullPath       string
	FullPathCrumbs []string
	ParentName     string
	Parent         *TestObject
	Package        string
	Status         string
	CaseID         int
	Desc           string
	GoTestName     string
	IssueURL       string
	IssueTicket    string
	RawEvents      []*TestEvent
	StartTime      time.Time
	EndTime        time.Time
	Elapsed        time.Duration
	OutputBatch    []string
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
		k := UniqTestKey(e)
		testNames[k] = 1
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

// EventsToTestObjects parses event batches to construct TestObjects, extracting caseID, Description, Status and IssueURL
func EventsToTestObjects(events map[string][]*TestEvent) ([]*TestObject, map[string]*TestObject) {
	tests := make([]*TestObject, 0)
	testsByName := make(map[string]*TestObject)
	for _, eventsBatch := range events {
		t := &TestObject{}
		t.StartTime = eventsBatch[0].Time
		t.EndTime = eventsBatch[len(eventsBatch)-1].Time
		for _, e := range eventsBatch {
			t.Package = e.Package
			t.GoTestName = e.Test
			t.FullPath = UniqTestKey(e)
			t.FullPathCrumbs = breadCrumbsFromFullPath(t.FullPath)
			if len(t.FullPathCrumbs) > 1 {
				t.ParentName = t.FullPathCrumbs[len(t.FullPathCrumbs)-2]
			}
			if e.Action == "pass" || e.Action == "skip" || e.Action == "fail" {
				t.Status = strings.ToUpper(e.Action)
			}
			if e.Elapsed != 0 {
				t.Elapsed = time.Duration(e.Elapsed) * time.Second
				t.EndTime = t.StartTime.Add(time.Duration(e.Elapsed) * time.Second)
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
		testsByName[t.FullPath] = t
	}
	return tests, testsByName
}

// Getting earliest and latest times to use it in StartItem or FinishItem calls
func getTimeBounds(events []*TestEvent) (time.Time, time.Time) {
	return events[0].Time, events[len(events)-1].Time
}

func getTimeBoundsByElapsed(events []*TestEvent) (time.Time, time.Time) {
	startTime := events[0].Time
	finishTime := events[0].Time.Add(time.Duration(events[0].Elapsed) * time.Second)
	return startTime, finishTime
}

func (m *RPAgent) testrailTestcaseDesc(to *TestObject) string {
	return strconv.Itoa(to.CaseID) + " " + to.Desc
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

func eventsToObjects(events []*TestEvent) ([]*TestObject, map[string]*TestObject) {
	groupedTestEventsBatch := groupEventsByTest(events)
	//debugDumpEventsToFile(groupedTestEventsBatch)
	tos, tosByName := EventsToTestObjects(groupedTestEventsBatch)
	return tos, tosByName
}

func sortTestObjectsByStartTime(to []*TestObject) {
	sort.SliceStable(to, func(i, j int) bool {
		return to[i].StartTime.Before(to[j].StartTime)
	})
}

func (m *RPAgent) Report(jsonFilename string, runName string, projectName string, tag string) {
	f, err := os.Open(jsonFilename)
	if err != nil {
		m.l.Fatal(err)
	}
	events := parseEventsBatch(f)
	testObjects, tosByName := eventsToObjects(events)
	m.l.Infof(InfoColor, fmt.Sprintf("sending report to: %s, project: %s", m.c.GetBaseUrl(), m.c.GetProject()))

	alreadyStartedTestEntities := make(map[string]*RPTestEntity)
	mustFinishTestEntities := make([]*RPTestEntity, 0)

	earliestInReport, latestInReport := getTimeBounds(events)
	var tags []string
	if tag == "" {
		tags = nil
	} else {
		tags = strings.Split(tag, ",")
	}
	launchData, err := m.c.StartLaunch(runName, runName, earliestInReport.Format(time.RFC3339), tags, "DEFAULT")
	if err != nil {
		m.l.Fatalf("error creating launch: %s", err)
	}

	sortTestObjectsByStartTime(testObjects)
	for _, to := range testObjects {
		parent := ""
		itemType := "TEST"
		for tIdx, tpath := range to.FullPathCrumbs {
			// start test items, starting from parents to child, add to alreadyStartedTestEntities
			if _, ok := alreadyStartedTestEntities[tpath]; !ok {
				testObj := tosByName[tpath]
				startTime := testObj.StartTime.Format(time.RFC3339)
				endTime := testObj.EndTime.Format(time.RFC3339)
				if len(strings.Split(tpath, "|")) == 1 {
					m.l.Debugf("module found, setting test startTime = launch startTime")
					startTime = earliestInReport.Format(time.RFC3339)
				}
				m.l.Debugf("starting new test entity:\n tpath: %s\n parent: %s\n duration: %d\nstart: %s\n end: %s\n",
					tpath,
					to.ParentName,
					to.Elapsed,
					startTime,
					endTime,
				)
				if tIdx > 0 {
					parentName := testObj.ParentName
					parent = alreadyStartedTestEntities[parentName].TestItemId
					itemType = "STEP"
				}
				startData, err := m.c.StartTestItemId(parent, tpath, itemType, startTime, tpath, nil, nil)
				if err != nil {
					log.Fatal(err)
				}
				m.l.Debugf("test started: name: %s, id: %s\n", tpath, startData.Id)
				te := &RPTestEntity{
					startData.Id,
					strconv.Itoa(launchData.Number),
					startData.UniqueId,
					to.IssueTicket,
					to.IssueURL,
					endTime,
					to.Status,
					0,
				}
				alreadyStartedTestEntities[tpath] = te
				mustFinishTestEntities = append(mustFinishTestEntities, te)

				m.l.Debugf("uploading logs to id: %s\n", startData.Id)
				_, err = m.c.LogId(startData.Id, strings.Join(to.OutputBatch, ""), "INFO")
				if err != nil {
					log.Fatal(err)
				}
			}
		}
	}
	for _, startedObj := range mustFinishTestEntities {
		stat := eventActionStatusToRPStatus(startedObj.Status)
		if stat == "" {
			m.l.Infof("status is empty, setting FAILED")
			stat = "FAILED"
		}
		var issue map[string]interface{}
		if startedObj.IssueURL != "" {
			issue = m.issuePayload(startedObj)
		}
		if _, err := m.c.FinishTestItemId(startedObj.TestItemId, stat, startedObj.EndTime, issue); err != nil {
			log.Fatal(err)
		}
	}
	// status is calculated automatically
	resp, err := m.c.FinishLaunch("FAILED", latestInReport.Format(time.RFC3339))
	if err != nil {
		m.l.Fatal(err)
	}
	m.linkIssues(mustFinishTestEntities)
	m.l.Infof(InfoColor, fmt.Sprintf("report launch url: %s", resp.Link))
}

func (m *RPAgent) linkIssues(e []*RPTestEntity) {
	for _, startedObj := range e {
		if startedObj.IssueURL != "" {
			res, err := m.c.GetItemIdByUUID(startedObj.TestItemId)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Printf("res of link: %v\n", res)
			if startedObj.Status == "PASS" {
				fmt.Printf("cannot link issue in PASSED test")
				continue
			}
			if _, err := m.c.LinkIssue(res.Id, startedObj.IssueTicket, startedObj.IssueURL); err != nil {
				log.Fatal(err)
			}
		}
	}
}

func (m *RPAgent) issuePayload(startedObj *RPTestEntity) map[string]interface{} {
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
	m.l.Infof("linking item with issue:\nlaunchNumber: %s\ntestItemId: %s\nuniqId: %s",
		startedObj.LaunchNumber,
		startedObj.TestItemId,
		startedObj.UniqTestItemId,
	)
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
