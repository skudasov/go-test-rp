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
| --rp_project  | -                            | RP project name        |
| --rp_run_name | -                            | RP run name            |
| --rp_url      | -                            | RP url                 |
| --rp_token    | -                            | RP token (uuid)        |
| --rp_tags     | -                            | test run tags          |
| --force       | -                            | exit with 0 anyway     |

```
go-test-rp --json_report testdata/parallel-report.json\
 --rp_run_name runName --rp_url https://rp.dev.insolar.io\
  --rp_project ***\
   --rp_token ***\
   --rp_tags e2e,someothertag\
    --force
```

You can log issues in tests like
```go
t.Logf("https://insolar.atlassian.net/browse/SAIV-986")
```
by default all issues are PRODUCT_BUG type
