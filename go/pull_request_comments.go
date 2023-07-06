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

	"github.com/google/go-github/v53/github"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/pkg/errors"
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
