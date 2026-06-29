package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/thefcan/k8s-resilience-harness/internal/buildinfo"
)

func TestRunVersion(t *testing.T) {
	var out, errBuf bytes.Buffer
	if err := run([]string{"--version"}, &out, &errBuf); err != nil {
		t.Fatalf("run(--version): unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), buildinfo.Version) {
		t.Fatalf("version output %q does not contain %q", out.String(), buildinfo.Version)
	}
	if errBuf.Len() != 0 {
		t.Fatalf("--version should not log to stderr, got %q", errBuf.String())
	}
}

func TestRunStartupLogsToStderr(t *testing.T) {
	var out, errBuf bytes.Buffer
	if err := run(nil, &out, &errBuf); err != nil {
		t.Fatalf("run(): unexpected error: %v", err)
	}
	if !strings.Contains(errBuf.String(), "harness starting") {
		t.Fatalf("expected startup log on stderr, got %q", errBuf.String())
	}
	if out.Len() != 0 {
		t.Fatalf("startup should not write to stdout, got %q", out.String())
	}
}

func TestRunRejectsBadFlags(t *testing.T) {
	cases := map[string][]string{
		"bad level":  {"-log-level=loud"},
		"bad format": {"-log-format=yaml"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var out, errBuf bytes.Buffer
			if err := run(args, &out, &errBuf); err == nil {
				t.Fatalf("run(%v): expected error, got nil", args)
			}
		})
	}
}

func TestRunHelpIsNotAnError(t *testing.T) {
	var out, errBuf bytes.Buffer
	if err := run([]string{"-h"}, &out, &errBuf); err != nil {
		t.Fatalf("run(-h): expected nil error, got %v", err)
	}
	if !strings.Contains(errBuf.String(), "Usage:") {
		t.Fatalf("expected usage text on stderr, got %q", errBuf.String())
	}
}
