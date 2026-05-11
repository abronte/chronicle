package main

import (
	"bytes"
	"chronicle/internal"
	"strings"
	"testing"
)

func TestPrintHelp(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf)

	out := buf.String()
	if !strings.Contains(out, "Chronicle") {
		t.Error("help output should contain 'Chronicle'")
	}
	if !strings.Contains(out, "watch") {
		t.Error("help should mention watch command")
	}
	if !strings.Contains(out, "recent") {
		t.Error("help should mention recent command")
	}
	if !strings.Contains(out, "diffs") {
		t.Error("help should mention diffs command")
	}
	if !strings.Contains(out, "update") {
		t.Error("help should mention update command")
	}
}

func TestRunHelpCommand(t *testing.T) {
	var buf bytes.Buffer
	err := run([]string{"help"}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "Chronicle") {
		t.Error("expected help output")
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var buf bytes.Buffer
	err := run([]string{"nonexistent"}, &buf)
	if err == nil {
		t.Error("expected error for unknown command")
	}
	if !strings.Contains(buf.String(), "Chronicle") {
		t.Error("expected help output for unknown command")
	}
}

func TestRunWatchVersionShortFlag(t *testing.T) {
	var buf bytes.Buffer
	err := internal.Watch([]string{"-v"}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "chronicle 0.2.2") {
		t.Errorf("expected version output, got: %s", buf.String())
	}
}

func TestRunWatchVersionLongFlag(t *testing.T) {
	var buf bytes.Buffer
	err := internal.Watch([]string{"-version"}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "chronicle 0.2.2") {
		t.Errorf("expected version output, got: %s", buf.String())
	}
}

func TestRunWatchVersionViaRun(t *testing.T) {
	var buf bytes.Buffer
	err := run([]string{"watch", "-v"}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "chronicle 0.2.2") {
		t.Errorf("expected version output, got: %s", buf.String())
	}
}

func TestRunDiffsNoFile(t *testing.T) {
	var buf bytes.Buffer
	err := runDiffs([]string{}, &buf)
	if err == nil {
		t.Error("expected error for missing file path")
	}
	if !strings.Contains(err.Error(), "file path required") {
		t.Errorf("expected 'file path required' error, got: %v", err)
	}
}

func TestRunDefaultIsWatch(t *testing.T) {
	var buf bytes.Buffer
	err := run([]string{"-v"}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "chronicle 0.2.2") {
		t.Errorf("expected version output for default watch command, got: %s", buf.String())
	}
}
