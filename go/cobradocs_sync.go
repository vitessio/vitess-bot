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
	"net/http"

	"github.com/google/go-github/v53/github"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/vitess.io/vitess-bot/go/git"
	"github.com/vitess.io/vitess-bot/go/shell"
)

// synchronize cobradocs from main and release branches
func (h *PullRequestHandler) synchronizeCobraDocs(
	ctx context.Context,
	client *github.Client,
	vitess *git.Repo,
	website *git.Repo,
	pr *github.PullRequest,
	prInfo prInformation,
) (*github.PullRequest, error) {
	logger := zerolog.Ctx(ctx)
	op := "update cobradocs"
	branch := "prod"
	headBranch := fmt.Sprintf("synchronize-cobradocs-for-%d", pr.GetNumber())
	headRef := fmt.Sprintf("refs/heads/%s", headBranch)

	prodBranch, _, err := client.Repositories.GetBranch(ctx, website.Owner, website.Name, branch, false)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed get production branch on %s/%s to update cobradocs on Pull Request %d", website.Owner, website.Name, pr.GetNumber())
	}

	baseTree := prodBranch.GetCommit().Commit.Tree.GetSHA()
	parent := prodBranch.GetCommit().GetSHA()
	var openPR *github.PullRequest

	if err := createAndCheckoutBranch(ctx, client, website, branch, headBranch, fmt.Sprintf("%s on Pull Request %d", op, pr.GetNumber())); err != nil {
		return nil, err
	}

	if err := setupRepo(ctx, vitess, fmt.Sprintf("%s on Pull Request %d", op, prInfo.num)); err != nil {
		return nil, err
	}

	prs, err := website.FindPRs(ctx, client, github.PullRequestListOptions{
		State:     "open",
		Head:      fmt.Sprintf("%s:%s", website.Owner, headBranch),
		Base:      branch,
		Sort:      "created",
		Direction: "desc",
	}, func(pr *github.PullRequest) bool {
		return pr.GetUser().GetLogin() == h.botLogin
	}, 1)
	if err != nil {
		return nil, err
	}

	if len(prs) != 0 {
		openPR = prs[0]
		baseRepo := openPR.GetBase().GetRepo()
		logger.Debug().Msgf("Using existing PR #%d (%s/%s:%s)", openPR.GetNumber(), baseRepo.GetOwner().GetLogin(), baseRepo.GetName(), headBranch)

		// If branch already existed, hard reset to `prod`.
		if err := website.ResetHard(ctx, branch); err != nil {
			return nil, errors.Wrapf(err, "Failed to reset %s to %s to %s for %s", headBranch, branch, op, pr.GetHTMLURL())
		}
	}

	if err := vitess.FetchRef(ctx, "origin", "--tags"); err != nil {
		return nil, errors.Wrapf(err, "Failed to fetch tags in repository %s/%s to %s on Pull Request %d", vitess.Owner, vitess.Name, op, prInfo.num)
	}

	// Run the sync script (which authors the commit locally but not with GitHub auth ctx).
	if _, err := shell.NewContext(ctx, "./tools/sync_cobradocs.sh").InDir(website.LocalDir).WithExtraEnv(
		fmt.Sprintf("VITESS_DIR=%s", vitess.LocalDir),
		"COBRADOCS_SYNC_PERSIST=yes",
	).Output(); err != nil {
		return nil, errors.Wrapf(err, "Failed to run cobradoc sync script in repository %s/%s to %s on Pull Request %d", website.Owner, website.Name, op, prInfo.num)
	}

	// Create a tree of the commit above using the GitHub API and then commit it.
	_, commit, err := h.writeAndCommitTree(
		ctx,
		client,
		website,
		pr,
		branch,
		"HEAD",
		baseTree,
		parent,
		fmt.Sprintf("synchronize cobradocs with %s/%s#%d", vitess.Owner, vitess.Name, pr.GetNumber()),
		op,
	)
	if err != nil {
		return nil, err
	}

	// Push the branch.
	if _, _, err := client.Git.UpdateRef(ctx, website.Owner, website.Name, &github.Reference{
		Ref:    &headRef,
		Object: &github.GitObject{SHA: commit.SHA},
	}, true); err != nil {
		return nil, errors.Wrapf(err, "Failed to force-push %s to %s on Pull Request %s", headBranch, op, pr.GetHTMLURL())
	}

	switch openPR {
	case nil:
		// Create a Pull Request for the new branch.
		newPR := &github.NewPullRequest{
			Title:               github.String(fmt.Sprintf("[cobradocs] synchronize with %s (vitess#%d)", pr.GetTitle(), pr.GetNumber())),
			Head:                github.String(headBranch),
			Base:                github.String(branch),
			Body:                github.String(fmt.Sprintf("## Description\nThis is an automated PR to synchronize the cobradocs with %s", pr.GetHTMLURL())),
			MaintainerCanModify: github.Bool(true),
		}
		newPRCreated, _, err := client.PullRequests.Create(ctx, website.Owner, website.Name, newPR)
		if err != nil {
			return nil, errors.Wrapf(err, "Failed to create Pull Request using branch %s on %s/%s", headBranch, website.Owner, website.Name)
		}

		return newPRCreated, nil
	default:
		// Edit the title and body to take us out of preview-mode.
		if _, _, err := client.PullRequests.Edit(ctx, website.Owner, website.Name, openPR.GetNumber(), &github.PullRequest{
			Title: github.String(fmt.Sprintf("[cobradocs] synchronize with %s (vitess#%d)", pr.GetTitle(), pr.GetNumber())),
			Body:  github.String(fmt.Sprintf("## Description\nThis is an automated PR to synchronize the cobradocs with %s", pr.GetHTMLURL())),
		}); err != nil {
			return nil, errors.Wrapf(err, "Failed to edit PR title/body on %s", openPR.GetHTMLURL())
		}

		if _, _, err := client.Issues.CreateComment(ctx, website.Owner, website.Name, openPR.GetNumber(), &github.IssueComment{
			Body: github.String(fmt.Sprintf("PR was force-pushed to resync changes after merge of vitess PR %s. Removing do-not-merge label.", pr.GetHTMLURL())),
		}); err != nil {
			return nil, errors.Wrapf(err, "Failed to add PR comment on %s", openPR.GetHTMLURL())
		}

		// Remove the doNotMerge label.
		if resp, err := client.Issues.RemoveLabelForIssue(ctx, website.Owner, website.Name, openPR.GetNumber(), doNotMergeLabel); err != nil {
			// We get a 404 if the label was already removed.
			if resp.StatusCode != http.StatusNotFound {

				return nil, errors.Wrapf(err, "Failed to remove %s label to %s", doNotMergeLabel, openPR.GetHTMLURL())
			}
		}

		return openPR, nil
	}
}

func createAndCheckoutBranch(ctx context.Context, client *github.Client, repo *git.Repo, baseBranch string, newBranch string, op string) error {
	baseRef, _, err := client.Git.GetRef(ctx, repo.Owner, repo.Name, "heads/"+baseBranch)
	if err != nil {
		return errors.Wrapf(err, "Failed to fetch %s ref for repository %s/%s to %s", baseBranch, repo.Owner, repo.Name, op)
	}

	if _, err := repo.CreateBranch(ctx, client, baseRef, newBranch); err != nil {
		errors.Wrapf(err, "Failed to create git ref %s for repository %s/%s to %s", newBranch, repo.Owner, repo.Name, op)
	}

	if err = setupRepo(ctx, repo, op); err != nil {
		return err
	}

	if err = repo.Checkout(ctx, newBranch); err != nil {
		return errors.Wrapf(err, "Failed to checkout %s/%s to %s to %s", repo.Owner, repo.Name, newBranch, op)
	}

	return nil
}

func setupRepo(ctx context.Context, repo *git.Repo, op string) error {
	if err := repo.Clone(ctx); err != nil {
		return errors.Wrapf(err, "Failed to clone repository %s/%s to %s", repo.Owner, repo.Name, op)
	}

	if err := repo.Clean(ctx); err != nil {
		return errors.Wrapf(err, "Failed to clean the repository %s/%s to %s", repo.Owner, repo.Name, op)
	}

	if err := repo.Checkout(ctx, repo.DefaultBranch); err != nil {
		return errors.Wrapf(err, "Failed to checkout %s in %s/%s to %s", repo.DefaultBranch, repo.Owner, repo.Name, op)
	}

	if err := repo.Fetch(ctx, "origin"); err != nil {
		return errors.Wrapf(err, "Failed to fetch origin on repository %s/%s to %s", repo.Owner, repo.Name, op)
	}

	if err := repo.ResetHard(ctx, "FETCH_HEAD"); err != nil {
		return errors.Wrapf(err, "Failed to reset the repository %s/%s to %s", repo.Owner, repo.Name, op)
	}

	return nil
}
