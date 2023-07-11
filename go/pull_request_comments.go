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
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/google/go-github/v53/github"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/pkg/errors"
)

const (
	errorCodePrefixLabel = "<!-- start -->"
	errorCodeSuffixLabel = "<!-- end -->"
)

var (
	alwaysAddLabels = []string{
		"NeedsWebsiteDocsUpdate",
		"NeedsDescriptionUpdate",
		"NeedsIssue",
	}
)

type PRCommentHandler struct {
	githubapp.ClientCreator

	reviewChecklist string
}

type prInformation struct {
	repo      *github.Repository
	num       int
	repoOwner string
	repoName  string
}

func getPRInformation(event github.PullRequestEvent) prInformation {
	repo := event.GetRepo()
	return prInformation{
		repo:      repo,
		num:       event.GetNumber(),
		repoOwner: repo.GetOwner().GetLogin(),
		repoName:  repo.GetName(),
	}
}

func (h *PRCommentHandler) Handles() []string {
	return []string{"pull_request"}
}

func (h *PRCommentHandler) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {
	var event github.PullRequestEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.Wrap(err, "failed to parse issue comment event payload")
	}

	switch event.GetAction() {
	case "opened":
		prInfo := getPRInformation(event)
		err := h.addReviewChecklist(ctx, event, prInfo)
		if err != nil {
			return err
		}
		err = h.addLabels(ctx, event, prInfo)
		if err != nil {
			return err
		}
		err = h.createErrorDocumentation(ctx, event, prInfo)
		if err != nil {
			return err
		}
	case "synchronize":
		prInfo := getPRInformation(event)
		err := h.createErrorDocumentation(ctx, event, prInfo)
		if err != nil {
			return err
		}
	}
	return nil
}

func (h *PRCommentHandler) addReviewChecklist(ctx context.Context, event github.PullRequestEvent, prInfo prInformation) error {
	installationID := githubapp.GetInstallationIDFromEvent(&event)

	client, err := h.NewInstallationClient(installationID)
	if err != nil {
		return err
	}

	ctx, logger := githubapp.PreparePRContext(ctx, installationID, prInfo.repo, event.GetNumber())

	prComment := github.IssueComment{
		Body: &h.reviewChecklist,
	}

	logger.Debug().Msgf("Adding review checklist to Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	if _, _, err := client.Issues.CreateComment(ctx, prInfo.repoOwner, prInfo.repoName, prInfo.num, &prComment); err != nil {
		logger.Error().Err(err).Msgf("Failed to comment the review checklist to Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	}
	return nil
}

func (h *PRCommentHandler) addLabels(ctx context.Context, event github.PullRequestEvent, prInfo prInformation) error {
	installationID := githubapp.GetInstallationIDFromEvent(&event)

	client, err := h.NewInstallationClient(installationID)
	if err != nil {
		return err
	}

	ctx, logger := githubapp.PreparePRContext(ctx, installationID, prInfo.repo, event.GetNumber())

	logger.Debug().Msgf("Adding initial labels to Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	if _, _, err := client.Issues.AddLabelsToIssue(ctx, prInfo.repoOwner, prInfo.repoName, prInfo.num, alwaysAddLabels); err != nil {
		logger.Error().Err(err).Msgf("Failed to add initial labels to Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	}
	return nil
}

func (h *PRCommentHandler) createErrorDocumentation(ctx context.Context, event github.PullRequestEvent, prInfo prInformation) error {
	installationID := githubapp.GetInstallationIDFromEvent(&event)

	client, err := h.NewInstallationClient(installationID)
	if err != nil {
		return err
	}

	ctx, logger := githubapp.PreparePRContext(ctx, installationID, prInfo.repo, event.GetNumber())

	logger.Debug().Msgf("Listing changed files in Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)

	const perPage = 100
	var allFiles []*github.CommitFile
	cont := true
	for page := 1; cont; page++ {
		files, _, err := client.PullRequests.ListFiles(ctx, prInfo.repoOwner, prInfo.repoName, prInfo.num, &github.ListOptions{PerPage: perPage})
		if err != nil {
			logger.Error().Err(err).Msgf("Failed to list changed files in Pull Request %s/%s#%d - at page %d", prInfo.repoOwner, prInfo.repoName, prInfo.num, page)
			return nil
		}
		if len(files) < perPage {
			cont = false
		}
		allFiles = append(allFiles, files...)
	}
	changeDetected := false
	for _, file := range allFiles {
		if file.GetFilename() == "go/vt/vterrors/code.go" {
			changeDetected = true
			break
		}
	}
	if !changeDetected {
		logger.Debug().Msgf("No change detect to 'go/vt/vterrors/code.go' in Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
		return nil
	}
	logger.Debug().Msgf("Change detect to 'go/vt/vterrors/code.go' in Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)

	_, err = execCmd("", "git", "clone", fmt.Sprintf("https://github.com/%s/%s", prInfo.repoOwner, prInfo.repoName), "/tmp/vitess")
	if err != nil && !strings.Contains(err.Error(), "already exists and is not an empty directory") {
		logger.Error().Err(err).Msgf("Failed to clone repository %s/%s to generate error code on Pull Request %d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
		return nil
	}

	_, err = execCmd("/tmp/vitess", "git", "fetch", "origin", fmt.Sprintf("refs/pull/%d/head", prInfo.num))
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to fetch Pull Request %s/%s#%d to generate error code", prInfo.repoOwner, prInfo.repoName, prInfo.num)
		return nil
	}

	_, err = execCmd("/tmp/vitess", "git", "checkout", "FETCH_HEAD")
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to checkout on Pull Request %s/%s#%d to generate error code", prInfo.repoOwner, prInfo.repoName, prInfo.num)
		return nil
	}

	vterrorsgenVitessBytes, err := execCmd("/tmp/vitess", "go", "run", "./go/vt/vterrors/vterrorsgen")
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to run ./go/vt/vterrors/vterrorsgen on Pull Request %s/%s#%d to generate error code", prInfo.repoOwner, prInfo.repoName, prInfo.num)
		return nil
	}
	vterrorsgenVitess := string(vterrorsgenVitessBytes)

	_, err = execCmd("/tmp", "git", "clone", fmt.Sprintf("https://github.com/%s/website", prInfo.repoOwner))
	if err != nil && !strings.Contains(err.Error(), "already exists and is not an empty directory") {
		logger.Error().Err(err).Msgf("Failed to clone repository vitessio/website to generate error code on Pull Request %d", prInfo.num)
		return nil
	}

	_, err = execCmd("/tmp/website", "git", "pull")
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to fetch vitessio/website to generate error code on Pull Request %d", prInfo.num)
		return nil
	}

	_, err = execCmd("", "cp", "./tools/get_release_from_docs.sh", "/tmp/website")
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to copy ./tools/get_release_from_docs.sh to local clone of website repo to generate error code on Pull Request %d", prInfo.num)
		return nil
	}

	// get current version of the documentation and version of the PR's base

	currentVersionDocsBytes, err := execCmd("/tmp/website", "./get_release_from_docs.sh")
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to get current documentation version from config.toml in vitessio/website to generate error code on Pull Request %d", prInfo.num)
		return nil
	}
	currentVersionDocs := string(currentVersionDocsBytes)

	prDetails, _, err := client.PullRequests.Get(ctx, prInfo.repoOwner, prInfo.repoName, prInfo.num)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to get the details of Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
		return nil
	}

	base := prDetails.GetBase()
	if base == nil {
		logger.Error().Err(err).Msgf("Could not find the base of Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
		return nil
	}
	if strings.HasPrefix(base.GetRef(), "release-") && len(base.GetRef()) == len("release-00.0") {
		currentVersionDocs = strings.Split(base.GetRef(), "-")[1]
	}

	docPath := "/tmp/website/content/en/docs/" + currentVersionDocs + "/reference/errors/query-serving.md"
	queryServingErrorsBytes, err := execCmd("/tmp/website", "cat", docPath)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to cat the query serving error file (%s) to generate error code for Pull Request %d", docPath, prInfo.num)
		return nil
	}
	queryServingErrors := string(queryServingErrorsBytes)

	startIdx := strings.Index(queryServingErrors, errorCodePrefixLabel) + len(errorCodePrefixLabel)
	endIdx := strings.Index(queryServingErrors, errorCodeSuffixLabel)
	newQueryServingError := strings.Replace(queryServingErrors, queryServingErrors[startIdx:endIdx], fmt.Sprintf("\n%s", vterrorsgenVitess), 1)

	err = os.WriteFile(docPath, []byte(newQueryServingError), os.ModePerm)
	if err != nil {
		logger.Error().Err(err).Msgf("Cannot write file (%s) to generate errors of Pull Request %d", docPath, prInfo.num)
		return nil
	}

	statusBytes, err := execCmd("/tmp/website", "git", "status", "-s")
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to do git status on vitessio/website to generate error code on Pull Request %d", prInfo.num)
		return nil
	}
	if len(statusBytes) == 0 {
		logger.Debug().Msgf("No change detected in error code in Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
		return nil
	}

	errorDocContentBytes, err := os.ReadFile(docPath)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed read final documentation file for error code generation on Pull Request %d", prInfo.num)
		return nil
	}
	errorDocContent := string(errorDocContentBytes)

	// create PR

	baseTree := ""
	parent := ""
	newBranch := false
	branchName := fmt.Sprintf("update-error-code-%d", prInfo.num)
	refName := "refs/heads/"+branchName
	branch, r, err := client.Repositories.GetBranch(ctx, prInfo.repoOwner, "website", branchName, false)
	if r.StatusCode != http.StatusNotFound && err != nil {
		logger.Error().Err(err).Msgf("Failed to get branch on vitessio/website to generate error code on Pull Request %d", prInfo.num)
		return nil
	}
	if r.StatusCode == http.StatusNotFound {
		newBranch = true

		prodBranch, _, err := client.Repositories.GetBranch(ctx, prInfo.repoOwner, "website", "prod", false)
		if err != nil {
			logger.Error().Err(err).Msgf("Failed get production branch on vitessio/website to generate error code on Pull Request %d", prInfo.num)
			return nil
		}

		baseTree = prodBranch.GetCommit().Commit.Tree.GetSHA()
		parent = prodBranch.GetCommit().GetSHA()

		_, _, err = client.Git.CreateRef(ctx, prInfo.repoOwner, "website", &github.Reference{
			Ref: &refName,
			Object: &github.GitObject{
				SHA:  &parent,
			},
		})
		if err != nil {
			logger.Error().Err(err).Msgf("Failed to create git ref on vitessio/website to generate error code on Pull Request %d", prInfo.num)
			return nil
		}
	} else {
		baseTree = branch.GetCommit().Commit.Tree.GetSHA()
		parent = branch.GetCommit().GetSHA()
	}


	blob := &github.Blob{
		Content:  github.String(errorDocContent),
		Encoding: github.String("utf-8"),
	}
	blob, _, err = client.Git.CreateBlob(ctx, prInfo.repoOwner, "website", blob)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed create blob to generate error code on Pull Request %d", prInfo.num)
		return nil
	}

	// Create a tree
	tree := &github.Tree{
		Entries: []*github.TreeEntry{
			{
				Path:    github.String(strings.TrimPrefix(docPath, "/tmp/website/")),
				Mode:    github.String("100644"),
				Type:    github.String("blob"),
				Content: github.String(errorDocContent),
			},
		},
	}
	tree, _, err = client.Git.CreateTree(ctx, prInfo.repoOwner, "website", baseTree, tree.Entries)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed create tree to generate error code on Pull Request %d", prInfo.num)
		return nil
	}

	// Create a commit
	commit := &github.Commit{
		Message: github.String("Updated the query-serving error code"),
		Tree:    tree,
		Parents: []*github.Commit{
			{SHA: &parent},
		},
	}
	commit, _, err = client.Git.CreateCommit(ctx, prInfo.repoOwner, "website", commit)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed create commit to generate error code on Pull Request %d", prInfo.num)
		return nil
	}

	// Update a reference
	ref := &github.Reference{
		Ref:    github.String(refName),
		Object: &github.GitObject{SHA: commit.SHA},
	}
	_, _, err = client.Git.UpdateRef(ctx, prInfo.repoOwner, "website", ref, true)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to update ref to generate error code on Pull Request %d", prInfo.num)
		return nil
	}

	if newBranch {
		// Create a Pull Request
		newPR := &github.NewPullRequest{
			Title:               github.String(fmt.Sprintf("Update error code documentation (#%d)", prInfo.num)),
			Head:                github.String(branchName),
			Base:                github.String("prod"),
			Body:                github.String(fmt.Sprintf("## Description\nThis Pull Request updates the error code documentation based on the changes made in https://github.com/%s/vitess/pull/%d", prInfo.repoOwner, prInfo.num)),
			MaintainerCanModify: github.Bool(true),
		}
		_, _, err = client.PullRequests.Create(ctx, prInfo.repoOwner, "website", newPR)
		if err != nil {
			logger.Error().Err(err).Msgf("Failed create PR to generate error code on Pull Request %d", prInfo.num)
			return nil
		}
	}
	return nil
}

func execCmd(dir, name string, arg ...string) ([]byte, error) {
	cmd := exec.Command(name, arg...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		execErr, ok := err.(*exec.ExitError)
		if ok {
			return nil, fmt.Errorf("%s:\nstderr: %s\nstdout: %s", err.Error(), execErr.Stderr, out)
		}
		return nil, err
	}
	return out, nil
}