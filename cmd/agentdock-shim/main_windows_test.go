//go:build windows

package main

import "testing"

func TestTrayRequiresWaitOnlyDetachesNormalBackgroundLaunches(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "no arguments", want: false},
		{name: "background", args: []string{"--background"}, want: false},
		{name: "background case insensitive", args: []string{" --BACKGROUND "}, want: false},
		{name: "task admin", args: []string{"--task-admin", "prepare-elevated"}, want: true},
		{name: "version", args: []string{"--version"}, want: true},
		{name: "help", args: []string{"--help"}, want: true},
		{name: "background plus management argument", args: []string{"--background", "--start-core"}, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := trayRequiresWait(test.args); got != test.want {
				t.Fatalf("trayRequiresWait(%q) = %v, want %v", test.args, got, test.want)
			}
		})
	}
}
