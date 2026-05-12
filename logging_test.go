package main

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func serverWithLogger(buf *bytes.Buffer, debug bool) *Server {
	return &Server{
		logger: log.New(buf, "", 0),
		debug:  debug,
	}
}

func TestDebugfOutputsWhenEnabled(t *testing.T) {
	var buf bytes.Buffer
	s := serverWithLogger(&buf, true)
	s.debugf("component=%s event=%s", "test", "started")
	out := buf.String()
	if !strings.Contains(out, "component=test") {
		t.Errorf("expected debug output to contain message, got: %q", out)
	}
	if !strings.Contains(out, "DEBUG") {
		t.Errorf("expected debug output to contain DEBUG prefix, got: %q", out)
	}
}

func TestDebugfSilentWhenDisabled(t *testing.T) {
	var buf bytes.Buffer
	s := serverWithLogger(&buf, false)
	s.debugf("should not appear")
	if buf.Len() != 0 {
		t.Errorf("expected no output when debug=false, got: %q", buf.String())
	}
}

func TestDebugfFormatsArgs(t *testing.T) {
	var buf bytes.Buffer
	s := serverWithLogger(&buf, true)
	s.debugf("issue=%d role=%s", 42, "developer")
	out := buf.String()
	if !strings.Contains(out, "issue=42") || !strings.Contains(out, "role=developer") {
		t.Errorf("expected formatted args in output, got: %q", out)
	}
}
