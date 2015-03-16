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
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

const (
	STDOUT = 1
	STDERR = 2
)

type LineInputCallback func(line string, payload int64)

type simpleGrowingBuffer struct {
	sync.Mutex
	byteBuffer        []byte
	LastNewlineOffset int
}

type CompositeWriter struct {
	Type int

	buffer          *simpleGrowingBuffer
	keepBuffer      bool // keep full buffer in memory
	callback        LineInputCallback
	callbackPayload int64
}

type CommandExecution struct {
	// request fields
	Command        string
	Args           []string
	Environment    map[string]string
	LogExecution   bool
	AutoReadStdout bool
	AutoReadStderr bool

	// result fields
	WrappedCmd   *exec.Cmd
	StdoutReader io.ReadCloser
	StderrReader io.ReadCloser
	Stdout       string
	Stderr       string
	ExitCode     int
}

var (
	ExtraSshOptions []string
)

func NewCompositeWriter(t int, cb LineInputCallback, payload int64, keepBuffer bool) *CompositeWriter {
	cw := CompositeWriter{Type: t}
	cw.callback = cb
	cw.callbackPayload = payload
	cw.keepBuffer = keepBuffer
	cw.buffer = &simpleGrowingBuffer{}
	return &cw
}

func (this *simpleGrowingBuffer) SliceLength() int {
	return len(this.byteBuffer) - this.LastNewlineOffset
}

func (this *simpleGrowingBuffer) Slice() []byte {
	return this.byteBuffer[this.LastNewlineOffset:]
}

func (this *simpleGrowingBuffer) SliceNext(count int) []byte {
	return this.byteBuffer[this.LastNewlineOffset : this.LastNewlineOffset+count]
}

func (this *simpleGrowingBuffer) Reset() {
	this.byteBuffer = []byte{}
	this.LastNewlineOffset = 0
}

func (this *simpleGrowingBuffer) Truncate() {
	this.byteBuffer = this.Slice()
	this.LastNewlineOffset = 0
}

func (this *simpleGrowingBuffer) IncrementOffset(delta int) {
	this.LastNewlineOffset += delta
}

func (this *simpleGrowingBuffer) Append(p []byte) {
	this.byteBuffer = append(this.byteBuffer, p...)
}

func (this *simpleGrowingBuffer) ToString() string {
	return string(this.byteBuffer)
}

// return full buffer - only valid in case keepBuffer is true
func (this CompositeWriter) ToString() string {
	if this.keepBuffer == false {
		panic("KeepBuffer = false for this composite writer")
	}

	return this.buffer.ToString()
}

func (this CompositeWriter) Write(p []byte) (n int, err error) {
	this.buffer.Lock()
	defer this.buffer.Unlock()

	// grow internal buffer
	this.buffer.Append(p)

	// no newline-tracking when no callback is present
	if this.callback != nil {
		foundSmth := false
		pos := bytes.IndexByte(this.buffer.Slice(), '\n')
		for pos != -1 {
			foundSmth = true
			// emit line
			line := string(this.buffer.SliceNext(pos))

			this.callback(line, this.callbackPayload)

			this.buffer.IncrementOffset(pos + 1)

			pos = bytes.IndexByte(this.buffer.Slice(), '\n')
		}

		if foundSmth && !this.keepBuffer {
			// truncate each time a new line is extracted
			this.buffer.Truncate()
		}
	}

	return len(p), nil
}

func (this CompositeWriter) Finalize() {
	this.buffer.Lock()
	defer this.buffer.Unlock()

	// no newline-tracking when no callback is present
	if this.callback != nil {
		pos := this.buffer.SliceLength()
		if pos > 0 {
			// emit line
			line := string(this.buffer.SliceNext(pos))
			this.callback(line, this.callbackPayload)

			if !this.keepBuffer {
				// reset each time a new line is extracted
				this.buffer.Reset()
			} else {
				this.buffer.IncrementOffset(pos)
			}
		}
	}
}

///
/// create a new command
///
func New(command string, args ...string) *CommandExecution {
	return &CommandExecution{
		Command:        command,
		Args:           args,
		AutoReadStdout: true,
		AutoReadStderr: true,
		Stdout:         "-- stdout: this command has never been executed --",
		Stderr:         "-- stderr: this command has never been executed --",
		ExitCode:       -1,
	}
}

func (self *CommandExecution) GetFormattedError() error {
	return fmt.Errorf("Process of command '%s %v' returned exit code %d, follows stdout and stderr\n%s\n%s", self.Command, self.Args, self.ExitCode, self.Stdout, self.Stderr)
}

func (self *CommandExecution) Begin() error {
	// create the native Go execution structure
	cmd := exec.Command(self.Command, self.Args...)

	if self.AutoReadStdout {
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}

		self.StdoutReader = stdout
	}

	if self.AutoReadStderr {
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return err
		}

		self.StderrReader = stderr
	}

	// apply custom environment (if any)
	if self.Environment != nil {
		cmd.Env = os.Environ()
		for k, v := range self.Environment {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	if self.LogExecution {
		log.Printf("DEBUG: execute \"%s %s\"", self.Command, strings.Join(self.Args, " "))
	}

	// initialize only when all other operations are successful
	self.WrappedCmd = cmd

	return nil
}

func (self *CommandExecution) Run() error {
	// auto-initialization
	if self.WrappedCmd == nil {
		err := self.Begin()
		if err != nil {
			return err
		}
	}

	// allow process to have been started externally
	if self.WrappedCmd.Process == nil {
		// actual execution of the process starts here
		if err := self.WrappedCmd.Start(); err != nil {
			return err
		}
	}

	var bytes []byte

	if self.StdoutReader != nil {
		var err error
		if bytes, err = ioutil.ReadAll(self.StdoutReader); err != nil {
			return err
		}
		self.Stdout = string(bytes)
	}

	if self.StderrReader != nil {
		var err error
		if bytes, err = ioutil.ReadAll(self.StderrReader); err != nil {
			return err
		}
		self.Stderr = string(bytes)
	}

	err := self.WrappedCmd.Wait()

	// finalize the reading of stdout/stderr via callbacks
	switch self.WrappedCmd.Stdout.(type) {
	case *CompositeWriter:
		cw := self.WrappedCmd.Stdout.(*CompositeWriter)
		cw.Finalize()

		// grab buffer
		if cw.keepBuffer {
			self.Stdout = cw.ToString()
			// allow GC of the attached byte buffer
			cw.buffer.Reset()
		}
	}

	switch self.WrappedCmd.Stderr.(type) {
	case *CompositeWriter:
		cw := self.WrappedCmd.Stderr.(*CompositeWriter)
		cw.Finalize()

		// grab buffer
		if cw.keepBuffer {
			self.Stderr = cw.ToString()
			// allow GC of the attached byte buffer
			cw.buffer.Reset()
		}
	}
	
	if err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			// The program has exited with an exit code != 0

			// This works on both Unix and Windows. Although package
			// syscall is generally platform dependent, WaitStatus is
			// defined for both Unix and Windows and in both cases has
			// an ExitStatus() method with the same signature.
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				self.ExitCode = status.ExitStatus()
			} else {
				// this should never happen, unless of a portability/Go issue
				panic("cannot retrieve exit code")
			}
		} else {
			// could not convert exit code, return general failure
			return err
		}
	} else {
		// set to 0, otherwise it would be the default -1
		self.ExitCode = 0
	}

	// success
	return nil
}

func (this *CommandExecution) RunWithCallbacks(stdoutCb, stderrCb LineInputCallback, stdoutPayload, stderrPayload int64, keepStdout, keepStderr bool) error {
	if stdoutCb != nil {
		this.AutoReadStdout = false
	}
	if stderrCb != nil {
		this.AutoReadStderr = false
	}

	// perform regular initialization - although most features are disabled
	err := this.Begin()
	if err != nil {
		return err
	}

	if stdoutCb != nil {
		this.WrappedCmd.Stdout = NewCompositeWriter(STDOUT, stdoutCb, stdoutPayload, keepStdout)
	}
	if stderrCb != nil {
		this.WrappedCmd.Stderr = NewCompositeWriter(STDERR, stderrCb, stderrPayload, keepStderr)
	}

	return this.Run()
}

///
/// following logic only affects SSH
///

func prepareSshArgs(host string, args ...string) []string {
	// add extra ssh options
	a := append(ExtraSshOptions, []string{"-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no", "-o", "LogLevel=ERROR", host}...)

	// add user specified options
	a = append(a, args...)

	return a
}

// the real server-side command is the first of the arguments
func NewSsh(host string, args ...string) *CommandExecution {
	cmd := New("ssh", prepareSshArgs(host, args...)...)

	return cmd
}
