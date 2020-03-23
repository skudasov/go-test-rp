package integration

import (
	"github.com/skudasov/rpgoclient"
	"go.uber.org/zap"
	"time"
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

type TestEvent struct {
	Time    time.Time
	Action  string
	Package string
	Test    string
	Elapsed float64
	Output  string
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

type RPTestItem struct {
	TestItemId     string
	LaunchNumber   string
	UniqTestItemId string
	IssueTicket    string
	IssueURL       string
	EndTime        string
	Status         string
	FailedTests    int
}

type TestRuntimeInfo struct {
	Parent string
	Issue  map[string]string
}
