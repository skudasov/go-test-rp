## go-test-rp
go-test json integration to Report Portal integration

#### Install
```
go get github.com/skudasov/go-test-rp
go install cmd/go-test-rp.go
```

#### Run
| Param key     |           Default            | Description            |
| ------------- | ---------------------------- | ---------------------- |
| --json_report | -                            | go test json file      |
| --log_level   | info                         | agent log level        |
| --rp_project  | -                            | RP project name        |
| --rp_run_name | -                            | RP run name            |
| --rp_run_desc | -                            | RP run description     |
| --rp_url      | -                            | RP url                 |
| --rp_token    | -                            | RP token (uuid)        |
| --rp_tags     | -                            | test run tags          |
| --force       | -                            | exit with 0 anyway     |

```
go-test-rp --json_report testdata/parallel-report.json\
 --rp_run_name runName\
 --rp_run_desc runDesc\
 --rp_url https://rp.dev.insolar.io\
 --rp_project ***\
 --rp_token ***\
 --rp_tags e2e,someothertag\
 --force
```
rp token is rp uuid you can find in profile

You can log issues in tests like
```go
t.Logf("issue_link:%s,issue_type:%s", issueLink, issueType)
```
by default all issues are PRODUCT_BUG type
