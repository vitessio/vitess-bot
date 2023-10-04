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
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/google/go-github/v53/github"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/vitess.io/vitess-bot/go/git"
)

const (
	backportLabelPrefix    = "Backport to: "
	forwardportLabelPrefix = "Forwardport to: "

	backport    = "backport"
	forwardport = "forwardport"
)

var (
	// these labels are added to PRs that are opened on vitess/vitess, and are not backports or forwardports
	alwaysAddLabels = []string{
		"NeedsWebsiteDocsUpdate",
		"NeedsDescriptionUpdate",
		"NeedsIssue",
	}
)

type PullRequestHandler struct {
	githubapp.ClientCreator

	reviewChecklist string

	vitessRepoLock  sync.Mutex
	websiteRepoLock sync.Mutex
}

func NewPullRequestHandler(cc githubapp.ClientCreator, reviewChecklist string) (h *PullRequestHandler, err error) {
	h = &PullRequestHandler{
		ClientCreator:   cc,
		reviewChecklist: reviewChecklist,
	}
	err = os.MkdirAll(h.Workdir(), 0777|os.ModeDir)

	return h, err
}

type prInformation struct {
	repo      *github.Repository
	num       int
	repoOwner string
	repoName  string
	merged    bool
	labels    []string
	base      *github.PullRequestBranch
	head      *github.PullRequestBranch
}

func getPRInformation(event github.PullRequestEvent) prInformation {
	repo := event.GetRepo()
	merged := false
	pr := event.GetPullRequest()
	if pr != nil {
		merged = pr.GetMerged()
	}
	var labels []string
	for _, label := range event.GetPullRequest().Labels {
		if label == nil {
			continue
		}
		labels = append(labels, label.GetName())
	}
	return prInformation{
		repo:      repo,
		num:       event.GetNumber(),
		repoOwner: repo.GetOwner().GetLogin(),
		repoName:  repo.GetName(),
		merged:    merged,
		labels:    labels,
		base:      event.GetPullRequest().GetBase(),
		head:      event.GetPullRequest().GetHead(),
	}
}

func (h *PullRequestHandler) Workdir() string {
	return filepath.Join("/", "tmp", "pull_request_handler")
}

func (h *PullRequestHandler) Handles() []string {
	return []string{"pull_request"}
}

func (h *PullRequestHandler) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {
	var event github.PullRequestEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.Wrap(err, "failed to parse issue comment event payload")
	}

	switch event.GetAction() {
	case "opened":
		prInfo := getPRInformation(event)
		if prInfo.repoName == "vitess" {
			err := h.addReviewChecklist(ctx, event, prInfo)
			if err != nil {
				return err
			}
			err = h.addLabels(ctx, event, prInfo)
			if err != nil {
				return err
			}
			err = h.createDocsPreview(ctx, event, prInfo)
			if err != nil {
				return err
			}
			err = h.createErrorDocumentation(ctx, event, prInfo)
			if err != nil {
				return err
			}
		}
	case "closed":
		prInfo := getPRInformation(event)
		if prInfo.merged && prInfo.repoName == "vitess" {
			err := h.backportPR(ctx, event, prInfo)
			if err != nil {
				return err
			}
			err = h.updateDocs(ctx, event, prInfo)
			if err != nil {
				return err
			}
		}
	case "synchronize":
		prInfo := getPRInformation(event)
		if prInfo.repoName == "vitess" {
			err := h.createDocsPreview(ctx, event, prInfo)
			if err != nil {
				return err
			}
			err = h.createErrorDocumentation(ctx, event, prInfo)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func panicHandler(logger zerolog.Logger) error {
	if err := recover(); err != nil {
		logger.Error().Msgf("%v\n%s\n", err, debug.Stack())
		if err, ok := err.(error); ok {
			return err
		}
	}

	return nil
}

func (h *PullRequestHandler) addReviewChecklist(ctx context.Context, event github.PullRequestEvent, prInfo prInformation) (err error) {
	installationID := githubapp.GetInstallationIDFromEvent(&event)

	client, err := h.NewInstallationClient(installationID)
	if err != nil {
		return err
	}

	ctx, logger := githubapp.PreparePRContext(ctx, installationID, prInfo.repo, event.GetNumber())
	defer func() {
		if e := panicHandler(logger); e != nil {
			err = e
		}
	}()

	prComment := github.IssueComment{
		Body: &h.reviewChecklist,
	}

	logger.Debug().Msgf("Adding review checklist to Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	if _, _, err := client.Issues.CreateComment(ctx, prInfo.repoOwner, prInfo.repoName, prInfo.num, &prComment); err != nil {
		logger.Error().Err(err).Msgf("Failed to comment the review checklist to Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	}
	return nil
}

func (h *PullRequestHandler) addLabels(ctx context.Context, event github.PullRequestEvent, prInfo prInformation) (err error) {
	installationID := githubapp.GetInstallationIDFromEvent(&event)
	ctx, logger := githubapp.PreparePRContext(ctx, installationID, prInfo.repo, event.GetNumber())
	defer func() {
		if e := panicHandler(logger); e != nil {
			err = e
		}
	}()

	for _, label := range prInfo.labels {
		if strings.EqualFold(label, backport) || strings.EqualFold(label, forwardport) {
			logger.Debug().Msgf("Pull Request %s/%s#%d has label %s, skipping adding initial labels",
				prInfo.repoOwner, prInfo.repoName, prInfo.num, label)
			return nil
		}
	}

	client, err := h.NewInstallationClient(installationID)
	if err != nil {
		return err
	}

	logger.Debug().Msgf("Adding initial labels to Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	if _, _, err := client.Issues.AddLabelsToIssue(ctx, prInfo.repoOwner, prInfo.repoName, prInfo.num, alwaysAddLabels); err != nil {
		logger.Error().Err(err).Msgf("Failed to add initial labels to Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	}
	return nil
}

func (h *PullRequestHandler) createErrorDocumentation(ctx context.Context, event github.PullRequestEvent, prInfo prInformation) (err error) {
	installationID := githubapp.GetInstallationIDFromEvent(&event)

	client, err := h.NewInstallationClient(installationID)
	if err != nil {
		return err
	}

	ctx, logger := githubapp.PreparePRContext(ctx, installationID, prInfo.repo, event.GetNumber())
	defer func() {
		if e := panicHandler(logger); e != nil {
			err = e
		}
	}()

	if prInfo.repoName != "vitess" {
		logger.Debug().Msgf("Pull Request %s/%s#%d is not on a vitess repo, skipping error generation", prInfo.repoOwner, prInfo.repoName, prInfo.num)
		return nil
	}

	logger.Debug().Msgf("Listing changed files in Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	changeDetected, err := detectErrorCodeChanges(ctx, prInfo, client)
	if err != nil {
		logger.Err(err).Msg(err.Error())
		return nil
	}
	if !changeDetected {
		logger.Debug().Msgf("No change detect to 'go/vt/vterrors/code.go' in Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
		return nil
	}
	logger.Debug().Msgf("Change detect to 'go/vt/vterrors/code.go' in Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)

	vitess := &git.Repo{
		Owner:    prInfo.repoOwner,
		Name:     prInfo.repoName,
		LocalDir: filepath.Join(h.Workdir(), "vitess"),
	}
	h.vitessRepoLock.Lock()
	vterrorsgenVitess, err := cloneVitessAndGenerateErrors(ctx, vitess, prInfo)
	h.vitessRepoLock.Unlock()
	if err != nil {
		logger.Err(err).Msg(err.Error())
		return nil
	}

	website := &git.Repo{
		Owner:    prInfo.repoOwner,
		Name:     "website",
		LocalDir: filepath.Join(h.Workdir(), "website"),
	}

	h.websiteRepoLock.Lock()
	currentVersionDocs, err := cloneWebsiteAndGetCurrentVersionOfDocs(ctx, website, prInfo)
	h.websiteRepoLock.Unlock()
	if err != nil {
		logger.Err(err).Msg(err.Error())
		return nil
	}

	h.websiteRepoLock.Lock()
	errorDocContent, docPath, err := generateErrorCodeDocumentation(ctx, client, website, prInfo, currentVersionDocs, vterrorsgenVitess)
	h.websiteRepoLock.Unlock()
	if err != nil {
		logger.Err(err).Msg(err.Error())
		return nil
	}
	if errorDocContent == "" {
		logger.Debug().Msgf("No change detected in error code in Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
		return nil
	}

	err = createCommitAndPullRequestForErrorCode(ctx, website, prInfo, client, errorDocContent, docPath)
	if err != nil {
		logger.Err(err).Msg(err.Error())
	}
	return nil
}

func (h *PullRequestHandler) backportPR(ctx context.Context, event github.PullRequestEvent, prInfo prInformation) (err error) {
	installationID := githubapp.GetInstallationIDFromEvent(&event)

	client, err := h.NewInstallationClient(installationID)
	if err != nil {
		return err
	}

	ctx, logger := githubapp.PreparePRContext(ctx, installationID, prInfo.repo, event.GetNumber())
	defer func() {
		if e := panicHandler(logger); e != nil {
			err = e
		}
	}()

	pr, _, err := client.PullRequests.Get(ctx, prInfo.repoOwner, prInfo.repoName, prInfo.num)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to get Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
		return nil
	}

	var (
		backportBranches    []string // list of branches to which we must backport
		forwardportBranches []string // list of branches to which we must forward-port
		otherLabels         []string // will be used to apply the original PR's labels to the new PRs
	)
	for _, label := range pr.Labels {
		if label == nil {
			continue
		}
		if strings.HasPrefix(label.GetName(), backportLabelPrefix) {
			backportBranches = append(backportBranches, strings.Split(label.GetName(), backportLabelPrefix)[1])
		} else if strings.HasPrefix(label.GetName(), forwardportLabelPrefix) {
			forwardportBranches = append(forwardportBranches, strings.Split(label.GetName(), forwardportLabelPrefix)[1])
		} else {
			otherLabels = append(otherLabels, label.GetName())
		}
	}

	if len(backportBranches) > 0 {
		logger.Debug().Msgf("Will backport Pull Request %s/%s#%d to branches %v", prInfo.repoOwner, prInfo.repoName, prInfo.num, backportBranches)
	}
	if len(forwardportBranches) > 0 {
		logger.Debug().Msgf("Will forwardport Pull Request %s/%s#%d to branches %v", prInfo.repoOwner, prInfo.repoName, prInfo.num, forwardportBranches)
	}

	vitessRepo := &git.Repo{
		Owner:    prInfo.repoOwner,
		Name:     prInfo.repoName,
		LocalDir: filepath.Join(h.Workdir(), "vitess"),
	}
	mergedCommitSHA := pr.GetMergeCommitSHA()

	for _, branch := range backportBranches {
		h.vitessRepoLock.Lock()
		newPRID, err := portPR(ctx, client, vitessRepo, prInfo, pr, mergedCommitSHA, branch, backport, otherLabels)
		h.vitessRepoLock.Unlock()
		if err != nil {
			logger.Err(err).Msg(err.Error())
			continue
		}
		logger.Debug().Msgf("Opened backport Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, newPRID)
	}
	for _, branch := range forwardportBranches {
		h.vitessRepoLock.Lock()
		newPRID, err := portPR(ctx, client, vitessRepo, prInfo, pr, mergedCommitSHA, branch, forwardport, otherLabels)
		h.vitessRepoLock.Unlock()
		if err != nil {
			logger.Err(err).Msg(err.Error())
			continue
		}
		logger.Debug().Msgf("Opened forward Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, newPRID)
	}

	return nil
}

func (h *PullRequestHandler) createDocsPreview(ctx context.Context, event github.PullRequestEvent, prInfo prInformation) error {
	// TODO: Checks:
	// 1. Is a PR against either:
	// 	- vitessio/vitess:main
	//	- vitessio/vitess:release-\d+\.\d+
	// 2. PR contains changes to either `go/cmd/**/*.go` OR `go/flags/endtoend/*.txt`
	return nil
}

func (h *PullRequestHandler) updateDocs(ctx context.Context, event github.PullRequestEvent, prInfo prInformation) (err error) {
	installationID := githubapp.GetInstallationIDFromEvent(&event)
	client, err := h.NewInstallationClient(installationID)
	if err != nil {
		return err
	}

	ctx, logger := githubapp.PreparePRContext(ctx, installationID, prInfo.repo, event.GetNumber())
	defer func() {
		if e := panicHandler(logger); e != nil {
			err = e
		}
	}()

	// TODO: Checks:
	// - is vitessio/vitess:main branch OR is vitessio/vitess versioned tag (v\d+\.\d+\.\d+)
	// - PR contains changes to either `go/cmd/**/*.go` OR `go/flags/endtoend/*.txt`
	if prInfo.base.GetRef() != "main" {
		logger.Debug().Msgf("PR is merged to %s, not main, skipping website cobradocs sync", prInfo.base.GetRef())
		return nil
	}

	vitess := &git.Repo{
		Owner:    prInfo.repoOwner,
		Name:     prInfo.repoName,
		LocalDir: filepath.Join(h.Workdir(), "vitess"),
	}
	website := &git.Repo{
		Owner:    prInfo.repoOwner,
		Name:     "website",
		LocalDir: filepath.Join(h.Workdir(), "website"),
	}

	_, err = synchronizeCobraDocs(ctx, client, vitess, website, event.GetPullRequest(), prInfo)
	return err
}
