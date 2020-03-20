package main

import (
	"flag"
	"github.com/skudasov/go-test-rp/integration"
	"log"
	"os"
	"runtime/pprof"
)

func main() {
	jsonReportPath := flag.String("json_report", "", "path to go test json report")
	runName := flag.String("rp_run_name", "", "testrun name")
	rpProject := flag.String("rp_project", "", "report portal project")
	rpUrl := flag.String("rp_url", "", "report portal base url")
	rpToken := flag.String("rp_token", "", "report portal token")
	btsProject := flag.String("bts_project", "SAIV", "bug tracker system project (name of integration in rp)")
	btsUrl := flag.String("bts_url", "https://insolar.atlassian.net/", "bug tracker system root url")
	tagStr := flag.String("rp_tags", "", "tags for marking test launch")
	level := flag.String("log_level", "info", "verbose logs")
	cpuprofile := flag.String("cpu_profile", "", "write cpu profile to file")
	force := flag.Bool("force", false, "if true, exit code will be 0 even if errors")
	dumptransport := flag.Bool("dumptransport", false, "debug http with bodies")
	flag.Parse()
	if *jsonReportPath == "" {
		log.Fatal("provide path to go test json report file using --json_report ex.json")
	}
	if *runName == "" {
		log.Fatal("provide any viable run name, ex. -rp_run_name *your_project_name*")
	}
	if *rpProject == "" {
		log.Fatal("provide your report portal project name, ex. -rp_project")
	}
	if *rpUrl == "" {
		log.Fatal("provide your report portal url")
	}
	if *rpToken == "" {
		log.Fatal("provide your report portal token")
	}
	if *tagStr == "" {
		log.Fatal("provide your report portal tags separated by comma, ex. v0.0.1,unit")
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal(err)
		}
	}

	a := integration.NewRPAgent(*rpUrl, *rpProject, *rpToken, *btsProject, *btsUrl, *dumptransport, integration.WithForce(*force), integration.WithVerbosity(*level))
	err := a.Report(*jsonReportPath, *runName, *rpProject, *tagStr)
	if err != nil {
		log.Fatal(err)
	}
	pprof.StopCPUProfile()
	if a.JsonReportErrorsNum > 0 && !*force {
		// if any errors of broken tests are present fail build
		os.Exit(1)
	}
}
