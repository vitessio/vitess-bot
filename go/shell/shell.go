/*
Copyright 2023 The Vitess Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package shell

import (
	"context"
	"fmt"
	"os/exec"
)

type cmd exec.Cmd

// New returns a new command can be run.
func New(name string, arg ...string) *cmd {
	return NewContext(context.Background(), name, arg...)
}

// NewContext returns a new command that can be run with the given context.
func NewContext(ctx context.Context, name string, arg ...string) *cmd {
	return (*cmd)(exec.CommandContext(ctx, name, arg...))
}

// InDir updates the working directory of the command and returns it.
//
// Must be called before running the command.
func (c *cmd) InDir(dir string) *cmd {
	c.Dir = dir
	return c
}

// WithEnv updates the environment of the command and returns it.
//
// Must be called before running the command.
func (c *cmd) WithEnv(env ...string) *cmd {
	c.Env = env
	return c
}

// WithExtraEnv appends to the environment of the command and returns it.
//
// Must be called before running the command.
func (c *cmd) WithExtraEnv(env ...string) *cmd {
	c.Env = append(c.Env, env...)
	return c
}

// Run runs the command and returns an error, capturing stderr, if any.
func (c *cmd) Run() error {
	err := (*exec.Cmd)(c).Run()
	if err != nil {
		return wrapErr(err, nil)
	}

	return nil
}

// Output runs the command and returns the output from stdout. If any error
// occurs, stderr is captured as well.
func (c *cmd) Output() ([]byte, error) {
	out, err := (*exec.Cmd)(c).Output()
	if err != nil {
		return nil, wrapErr(err, out)
	}

	return out, nil
}

func wrapErr(err error, out []byte) error {
	if execErr, ok := err.(*exec.ExitError); ok {
		err := fmt.Errorf("%s\nstderr: %s", err.Error(), execErr.Stderr)
		if out != nil {
			err = fmt.Errorf("%s\nstdout: %s", err.Error(), out)
		}

		return err
	}

	return err
}
