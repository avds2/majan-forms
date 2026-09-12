package main

import (
	"strings"
	"testing"
)

func TestCommandHelpDoesNotRequireConfiguration(t *testing.T) {
	tests := []struct {
		arguments []string
		contains  string
	}{
		{[]string{"--help"}, "forms serve"},
		{[]string{"help"}, "forms serve"},
		{[]string{"admin", "--help"}, "reset-password"},
		{[]string{"admin", "create", "--help"}, "reset-password"},
		{[]string{"help", "doctor"}, "PostgreSQL"},
		{[]string{"backup", "-h"}, "retention"},
		{[]string{"restore", "help"}, "systemd"},
	}
	for _, test := range tests {
		help, ok := commandHelp(test.arguments)
		if !ok {
			t.Fatalf("commandHelp(%q) did not recognize help", test.arguments)
		}
		if !strings.Contains(help, test.contains) {
			t.Fatalf("commandHelp(%q)=%q, want it to contain %q", test.arguments, help, test.contains)
		}
	}
}

func TestKnownCommand(t *testing.T) {
	for command := range commandUsages {
		if !knownCommand(command) {
			t.Fatalf("known command %q was rejected", command)
		}
	}
	if knownCommand("unknown") {
		t.Fatal("unknown command was accepted")
	}
}
