/*
 * libshell v0.1.0 - Feature-rich shell library for Go systems integration projects
 * Copyright (C) 2014 gdm85 - https://github.com/gdm85/go-libshell/

This program is free software; you can redistribute it and/or
modify it under the terms of the GNU General Public License
as published by the Free Software Foundation; either version 2
of the License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program; if not, write to the Free Software
Foundation, Inc., 51 Franklin Street, Fifth Floor, Boston, MA  02110-1301, USA.
*/
package shell

import (
	"fmt"
	"testing"
)

func theCallback(line string, payload int64) {
	// NOP
}

func TestFalse(t *testing.T) {
	cmd := New("false")

	err := cmd.Run()
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	if cmd.ExitCode != 1 {
		t.Logf("false not working")
		t.FailNow()
	}

}

func TestExitCode(t *testing.T) {
	failed := 0
	for testVal := 0; testVal < 10; testVal++ {
		cmd := New("sh", "-c", fmt.Sprintf("exit %d", testVal))

		err := cmd.Run()
		if err != nil {
			t.Error(err)
			t.FailNow()
		}

		if cmd.ExitCode != testVal {
			t.Logf("expected exit code %d but %d received", testVal, cmd.ExitCode)
			failed++
		}
	}
	if failed > 0 {
		t.FailNow()
	}
}

func TestCallbacks(t *testing.T) {
	cmd := New("sh", "-c", "echo start; sleep 1; echo line on stdout; sleep 1; echo XXX line on stderr 1>&2; sleep 1; echo finished")

	err := cmd.RunWithCallbacks(theCallback, theCallback, 1, 2, true, true)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	t.Logf("stdout = %#v", cmd.Stdout)
	t.Logf("stderr = %#v", cmd.Stderr)

	if cmd.Stdout != "start\nline on stdout\nfinished\n" || cmd.Stderr != "XXX line on stderr\n" {
		t.Logf("Output mismatch")
		t.FailNow()
	}

	t.Logf("successfully executed with exit value = %d", cmd.ExitCode)
}
