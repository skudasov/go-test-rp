// Provides integration from go tool2json report to Report Portal
package integration

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/skudasov/rpgoclient"
	"io"
	"log"
	"os"
	"regexp"
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

const (
	TRUrl = "https://insolar.testrail.io/index.php?/cases/view/%d"
)

var (
	//issueRe           = regexp.MustCompile("issue_link:(.+),.*issue_type:(.+)")
	testStatusRe    = regexp.MustCompile(`--- (.*):`)
	testSkipIssueRe = regexp.MustCompile(`https://insolar\.atlassian\.net/browse/([A-Z]+-\d+)`)
	testCaseIdRe    = regexp.MustCompile(`C(\d{1,8})\s(.*)`)
)

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

func WithForce(force bool) func(client *RPAgent) error {
	return func(c *RPAgent) error {
		c.Force = force
		return nil
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

func eventsToObjects(events []*TestEvent) ([]*TestObject, map[string]*TestObject) {
	groupedTestEventsBatch := groupEventsByTest(events)
	//debugDumpEventsToFile(groupedTestEventsBatch)
	tos, tosByName := EventsToTestObjects(groupedTestEventsBatch)
	return tos, tosByName
}

func RPTestData(tpath string, testObj *TestObject) (name string, desc string) {
	if testObj.CaseID != 0 {
		name = testRailTestCase(testObj)
		desc = testRailTestDesc(testObj)
	} else {
		name = tpath
		desc = tpath
	}
	return
}

// startEntitiesHierarchy starts RP test entities hierarchically, using test path
func (m *RPAgent) startEntitiesHierarchy(
	launchData rpgoclient.StartLaunchResponse,
	events []*TestEvent,
	testObjects []*TestObject,
	tosByName map[string]*TestObject,
) []*RPTestItem {
	alreadyStartedTestEntities := make(map[string]*RPTestItem)
	mustFinishTestEntities := make([]*RPTestItem, 0)
	sortTestObjectsByStartTime(testObjects)
	earliestInReport, _ := getTimeBounds(events)
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
				tname, tdesc := RPTestData(tpath, testObj)
				startData, err := m.c.StartTestItemId(parent, tname, itemType, startTime, tdesc, nil, nil)
				if err != nil {
					log.Fatal(err)
				}
				m.l.Debugf("test started: name: %s, id: %s\n", tpath, startData.Id)
				te := &RPTestItem{
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
	return mustFinishTestEntities
}

func (m *RPAgent) finishEntities(e []*RPTestItem) {
	for _, startedObj := range e {
		stat := eventActionStatusToRPStatus(startedObj.Status)
		if stat == "" {
			m.l.Infof("status is empty, setting FAILED")
			stat = "FAILED"
		}
		var issue map[string]interface{}
		if startedObj.IssueURL != "" && startedObj.Status != "PASS" {
			issue = m.issuePayload(startedObj)
		}
		if _, err := m.c.FinishTestItemId(startedObj.TestItemId, stat, startedObj.EndTime, issue); err != nil {
			log.Fatal(err)
		}
	}
}

func (m *RPAgent) Report(jsonFilename string, runName string, projectName string, tag string) {
	f, err := os.Open(jsonFilename)
	if err != nil {
		m.l.Fatal(err)
	}
	events := parseEventsBatch(f)
	testObjects, tosByName := eventsToObjects(events)
	m.l.Infof(InfoColor, fmt.Sprintf("sending report to: %s, project: %s", m.c.GetBaseUrl(), m.c.GetProject()))
	earliestInReport, latestInReport := getTimeBounds(events)
	launchData, err := m.c.StartLaunch(runName, runName, earliestInReport.Format(time.RFC3339), parseTags(tag), "DEFAULT")
	if err != nil {
		m.l.Fatal(err)
	}
	mustFinishTestEntities := m.startEntitiesHierarchy(launchData, events, testObjects, tosByName)
	m.finishEntities(mustFinishTestEntities)
	// status is calculated automatically in RP 5.0.0, but any valid status still required
	resp, err := m.c.FinishLaunch("FAILED", latestInReport.Format(time.RFC3339))
	if err != nil {
		m.l.Fatal(err)
	}
	m.linkIssues(mustFinishTestEntities)
	m.l.Infof(InfoColor, fmt.Sprintf("report launch url: %s", resp.Link))
}

// linkIssues links issues via link api
// in RP 5.0.0 adding bts data in FinishItem payload doesn't link issue automatically
func (m *RPAgent) linkIssues(e []*RPTestItem) {
	for _, startedObj := range e {
		if startedObj.IssueURL != "" && startedObj.Status != "PASS" {
			res, err := m.c.GetItemIdByUUID(startedObj.TestItemId)
			if err != nil {
				log.Fatal(err)
			}
			m.l.Infof("linking item with issue:\nlaunchNumber: %s\ntestItemId: %s\nuniqId: %s",
				startedObj.LaunchNumber,
				startedObj.TestItemId,
				startedObj.UniqTestItemId,
			)
			if _, err := m.c.LinkIssue(res.Id, startedObj.IssueTicket, startedObj.IssueURL); err != nil {
				log.Fatal(err)
			}
		}
	}
}

// issuePayload creates payload with bug tracker system data
func (m *RPAgent) issuePayload(startedObj *RPTestItem) map[string]interface{} {
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
	return issue
}
