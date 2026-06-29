// Command harness is the CLI entrypoint for the k8s-resilience-harness.
//
// At milestone M0 it only wires up structured logging and reports build info;
// fault injection, probing and verdicts arrive in later milestones (M2+).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/thefcan/k8s-resilience-harness/internal/buildinfo"
	"github.com/thefcan/k8s-resilience-harness/internal/logger"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "harness:", err)
		os.Exit(1)
	}
}

// run is the testable entrypoint: all I/O is injected so behaviour can be
// asserted without touching real stdout/stderr or calling os.Exit.
func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("harness", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		showVersion bool
		logLevel    string
		logFormat   string
	)
	fs.BoolVar(&showVersion, "version", false, "print build version and exit")
	fs.StringVar(&logLevel, "log-level", "info", "log level: debug|info|warn|error")
	fs.StringVar(&logFormat, "log-format", "text", "log format: text|json")

	fs.Usage = func() {
		_, _ = fmt.Fprint(stderr,
			"k8s-resilience-harness — Kubernetes resilience/chaos testing harness\n\n"+
				"Usage:\n  harness [flags]\n\nFlags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		// -h/-help is not a failure: usage has already been printed.
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if showVersion {
		_, err := fmt.Fprintln(stdout, buildinfo.String())
		return err
	}

	level, err := logger.ParseLevel(logLevel)
	if err != nil {
		return err
	}
	format, err := logger.ParseFormat(logFormat)
	if err != nil {
		return err
	}

	log := logger.New(stderr, level, format)
	log.Info("harness starting",
		"version", buildinfo.Version,
		"commit", buildinfo.Commit,
	)
	log.Warn("no experiment runner wired yet — this is the M0 skeleton",
		"next_milestone", "M1: SUT + loadgen + baseline",
	)
	return nil
}
