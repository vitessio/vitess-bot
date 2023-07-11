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

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v53/github"
	"github.com/pkg/errors"
)

func portPR(ctx context.Context, client *github.Client, prInfo prInformation, pr *github.PullRequest, mergedCommitSHA, branch string) (int, error) {
	// Get a reference to the release branch
	releaseRef, _, err := client.Git.GetRef(ctx, prInfo.repoOwner, prInfo.repoName, fmt.Sprintf("heads/%s", branch))
	if err != nil {
		return 0, errors.Wrapf(err, "")
	}

	// Create a new branch from the release branch
	newBranch := fmt.Sprintf("port-%d-to-%s", pr.GetNumber(), branch)
	newRef := &github.Reference{
		Ref: github.String("refs/heads/" + newBranch),
		Object: &github.GitObject{
			SHA: releaseRef.GetObject().SHA,
		},
	}
	_, _, err = client.Git.CreateRef(ctx, prInfo.repoOwner, prInfo.repoName, newRef)
	if err != nil {
		return 0, errors.Wrapf(err, "")
	}


	// Clone the repository
	_, err = execCmd("", "git", "clone", fmt.Sprintf("git@github.com:%s/%s.git", prInfo.repoOwner, prInfo.repoName), "/tmp/vitess")
	if err != nil && !strings.Contains(err.Error(), "already exists and is not an empty directory") {
		return 0, errors.Wrapf(err, "Failed to clone repository %s/%s to backport Pull Request %d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	}

	// Fetch origin
	_, err = execCmd("/tmp/vitess", "git", "fetch", "origin")
	if err != nil {
		return 0, errors.Wrapf(err, "Failed to fetch origin on repository %s/%s to backport Pull Request %d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	}

	// Checkout the new branch
	_, err = execCmd("/tmp/vitess", "git", "checkout", newBranch)
	if err != nil {
		return 0, errors.Wrapf(err, "Failed to checkout repository %s/%s to branch %s to backport Pull Request %d", prInfo.repoOwner, prInfo.repoName, newBranch, prInfo.num)
	}

	// Cherry-pick the commit
	_, err = execCmd("/tmp/vitess", "git", "cherry-pick", "-m", "1", mergedCommitSHA)
	if err != nil && strings.Contains(err.Error(), "after resolving the conflicts, mark the corrected paths") {
		_, err = execCmd("/tmp/vitess", "git", "add", ".")
		if err != nil {
			return 0, errors.Wrapf(err, "Failed to do 'git add' on branch %s to backport Pull Request %d", newBranch, prInfo.num)
		}

		_, err = execCmd("/tmp/vitess", "git", "commit", "--author=\"vitess-bot[bot] <108069721+vitess-bot[bot]@users.noreply.github.com>\"", "-m", fmt.Sprintf("\"Cherry-pick %s with conflicts\"", mergedCommitSHA))
		if err != nil {
			return 0, errors.Wrapf(err, "Failed to do 'git commit' on branch %s to backport Pull Request %d", newBranch, prInfo.num)
		}
	} else if err != nil {
		return 0, errors.Wrapf(err, "Failed to cherry-pick %s to branch %s to backport Pull Request %d", mergedCommitSHA, newBranch, prInfo.num)
	} else {
		_, err = execCmd("/tmp/vitess", "git", "commit", "--amend", "--author=\"vitess-bot[bot] <108069721+vitess-bot[bot]@users.noreply.github.com>\"", "--no-edit")
		if err != nil {
			return 0, errors.Wrapf(err, "Failed to do 'git commit --amend' on branch %s to backport Pull Request %d", newBranch, prInfo.num)
		}
	}

	// Push the changes
	_, err = execCmd("/tmp/vitess", "git", "push", "origin", newBranch)
	if err != nil {
		return 0, errors.Wrapf(err, "Failed to push %s to backport Pull Request %s", newBranch, prInfo.num)
	}

	// Create a Pull Request for the new branch
	newPR := &github.NewPullRequest{
		Title:               github.String(fmt.Sprintf("[%s] %s (#%d)", branch, pr.GetTitle(), pr.GetNumber())),
		Head:                github.String(newBranch),
		Base:                github.String(branch),
		Body:                github.String(fmt.Sprintf("## Description\nThis is a backport of #%d", pr.GetNumber())),
		MaintainerCanModify: github.Bool(true),
	}
	newPRCreated, _, err := client.PullRequests.Create(ctx, prInfo.repoOwner, prInfo.repoName, newPR)
	if err != nil {
		return 0, errors.Wrapf(err, "")
	}

	return newPRCreated.GetNumber(), nil
}
