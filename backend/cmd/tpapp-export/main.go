// tpapp-export renders the AgentGuard core pipeline as a Timeplus app
// (apps/agentguard). It needs no Timeplus connection.
//
//	go run ./cmd/tpapp-export --out ../apps/agentguard
//	go run ./cmd/tpapp-export --out ../apps/agentguard --check   # exit 1 if out of date
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/timeplus/agentguard/internal/tpapp"
)

func main() {
	out := flag.String("out", "../apps/agentguard", "output directory (the app root)")
	check := flag.Bool("check", false, "do not write; exit 1 if the output directory is out of date")
	flag.Parse()

	app, err := tpapp.Build()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tpapp-export:", err)
		os.Exit(2)
	}
	if *check {
		diff, err := tpapp.Diff(app, *out)
		if err != nil {
			fmt.Fprintln(os.Stderr, "tpapp-export:", err)
			os.Exit(2)
		}
		fmt.Println(tpapp.FormatDiff(diff))
		if len(diff) > 0 {
			os.Exit(1)
		}
		return
	}
	if err := tpapp.Write(app, *out); err != nil {
		fmt.Fprintln(os.Stderr, "tpapp-export:", err)
		os.Exit(2)
	}
	fmt.Printf("wrote %d files to %s (%d resources)\n", len(app.Files), *out, len(app.Resources))
}
