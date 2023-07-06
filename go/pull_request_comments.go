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

type PRCommentHandler struct {
	githubapp.ClientCreator

	reviewChecklist string
}

func (h *PRCommentHandler) Handles() []string {
	return []string{"pull_request"}
}

func (h *PRCommentHandler) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {
	var event github.PullRequestEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.Wrap(err, "failed to parse issue comment event payload")
	}

	var err error
	switch event.GetAction() {
	case "opened":
		err = h.addReviewChecklist(ctx, event)
	}
	return err
}

func (h *PRCommentHandler) addReviewChecklist(ctx context.Context, event github.PullRequestEvent) error {
	repo := event.GetRepo()
	prNum := event.GetNumber()
	repoOwner := repo.GetOwner().GetLogin()
	repoName := repo.GetName()

	installationID := githubapp.GetInstallationIDFromEvent(&event)

	client, err := h.NewInstallationClient(installationID)
	if err != nil {
		return err
	}

	ctx, logger := githubapp.PreparePRContext(ctx, installationID, repo, event.GetNumber())

	prComment := github.IssueComment{
		Body: &h.reviewChecklist,
	}

	logger.Debug().Msgf("Adding review checklist on %s/%s#%d", repoOwner, repoName, prNum)
	if _, _, err := client.Issues.CreateComment(ctx, repoOwner, repoName, prNum, &prComment); err != nil {
		logger.Error().Err(err).Msg("Failed to comment on pull request")
	}
	return nil
}
