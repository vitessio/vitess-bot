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

package git

import (
	"context"
	"fmt"
	"strings"

	"github.com/vitess.io/vitess-bot/go/shell"
)

type Repo struct {
	Owner    string
	Name     string
	LocalDir string
}

func (r *Repo) Add(ctx context.Context, arg ...string) error {
	_, err := shell.NewContext(ctx, "git", append([]string{"add"}, arg...)...).Output()
	return err
}

func (r *Repo) Checkout(ctx context.Context, ref string) error {
	_, err := shell.NewContext(ctx, "git", "checkout", ref).InDir(r.LocalDir).Output()
	return err
}

func (r *Repo) CherryPickMerge(ctx context.Context, sha string) error {
	_, err := shell.NewContext(ctx, "git", append([]string{"cherry-pick", "-m", "1"}, sha)...).InDir(r.LocalDir).Output()
	return err
}

func (r *Repo) Clean(ctx context.Context) error {
	_, err := shell.NewContext(ctx, "git", "clean", "-fd").InDir(r.LocalDir).Output()
	return err
}

func (r *Repo) Clone(ctx context.Context) error {
	_, err := shell.NewContext(ctx, "git", "clone", fmt.Sprintf("https://github.com/%s/%s.git", r.Owner, r.Name), r.LocalDir).Output()
	if err != nil && !strings.Contains(err.Error(), "already exists and is not an empty directory") {
		return err
	}

	return nil
}

type CommitOpts struct {
	Author string

	Amend  bool
	NoEdit bool
}

func (r *Repo) Commit(ctx context.Context, msg string, opts CommitOpts) error {
	args := []string{
		"commit",
	}

	if !opts.NoEdit {
		args = append(args, "-m", msg)
	} else {
		args = append(args, "--no-edit")
	}

	if opts.Author != "" {
		args = append(args, fmt.Sprintf("--author=%q", opts.Author))
	}

	if opts.Amend {
		args = append(args, "--amend")
	}

	_, err := shell.NewContext(ctx, "git", args...).Output()
	return err
}

func (r *Repo) Fetch(ctx context.Context, remote string) error {
	return r.fetch(ctx, remote)
}

func (r *Repo) FetchRef(ctx context.Context, remote, ref string) error {
	return r.fetch(ctx, remote, ref)
}

func (r *Repo) fetch(ctx context.Context, arg ...string) error {
	_, err := shell.NewContext(ctx, "git", append([]string{"fetch"}, arg...)...).InDir(r.LocalDir).Output()
	return err
}

func (r *Repo) Pull(ctx context.Context) error {
	_, err := shell.NewContext(ctx, "git", "pull").InDir(r.LocalDir).Output()
	return err
}

type PushOpts struct {
	Remote         string
	Refs           []string
	Force          bool
	ForceWithLease bool
}

func (r *Repo) Push(ctx context.Context, opts PushOpts) error {
	args := []string{
		"push",
	}

	switch {
	case opts.ForceWithLease:
		args = append(args, "--force-with-lease")
	case opts.Force:
		args = append(args, "--force")
	}

	if opts.Remote != "" {
		args = append(args, opts.Remote)
		if len(opts.Refs) > 0 {
			args = append(args, opts.Refs...)
		}
	}

	_, err := shell.NewContext(ctx, "git", args...).Output()
	return err
}

func (r *Repo) ResetHard(ctx context.Context, ref string) error {
	_, err := shell.NewContext(ctx, "git", append([]string{"reset", "--hard"}, ref)...).InDir(r.LocalDir).Output()
	return err
}

func (r *Repo) Status(ctx context.Context, arg ...string) ([]byte, error) {
	return shell.NewContext(ctx, "git", append([]string{"status"}, arg...)...).InDir("/tmp/website").Output()
}
