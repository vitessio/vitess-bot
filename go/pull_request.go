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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/google/go-github/v53/github"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/vitess.io/vitess-bot/go/git"
	"github.com/vitess.io/vitess-bot/go/shell"
)

const (
	backportLabelPrefix    = "Backport to: "
	forwardportLabelPrefix = "Forwardport to: "

	backport    = "backport"
	forwardport = "forwardport"

	doNotMergeLabel = "do-not-merge"
)

var (
	// these labels are added to PRs that are opened on vitess/vitess, and are not backports or forwardports
	alwaysAddLabels = []string{
		"NeedsWebsiteDocsUpdate",
		"NeedsDescriptionUpdate",
		"NeedsIssue",
		"NeedsBackportReason",
	}
)

type PullRequestHandler struct {
	githubapp.ClientCreator

	botLogin        string
	reviewChecklist string

	vitessRepoLock  sync.Mutex
	websiteRepoLock sync.Mutex
}

func NewPullRequestHandler(cc githubapp.ClientCreator, reviewChecklist, botLogin string) (h *PullRequestHandler, err error) {
	h = &PullRequestHandler{
		ClientCreator:   cc,
		botLogin:        botLogin,
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

	var err error
	switch event.GetAction() {
	case "opened":
		err = h.openedPullRequest(ctx, event)
	case "closed":
		err = h.closedPullRequest(ctx, event)
	case "labeled":
		err = h.labeledPullRequest(ctx, event)
	case "synchronize":
		err = h.synchronizePullRequest(ctx, event)
	}
	return err
}

func (h *PullRequestHandler) openedPullRequest(ctx context.Context, event github.PullRequestEvent) error {
	prInfo := getPRInformation(event)
	if prInfo.repoName != "vitess" {
		return nil
	}

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
	return nil
}

func (h *PullRequestHandler) closedPullRequest(ctx context.Context, event github.PullRequestEvent) error {
	prInfo := getPRInformation(event)
	if prInfo.repoName != "vitess" || !prInfo.merged {
		return nil
	}

	err := h.backportPR(ctx, event, prInfo)
	if err != nil {
		return err
	}
	return nil
}

func (h *PullRequestHandler) labeledPullRequest(ctx context.Context, event github.PullRequestEvent) error {
	prInfo := getPRInformation(event)
	if prInfo.repoName != "vitess" {
		return nil
	}

	err := h.addArewefastyetComment(ctx, event, prInfo)
	if err != nil {
		return err
	}
	return nil
}

func (h *PullRequestHandler) synchronizePullRequest(ctx context.Context, event github.PullRequestEvent) error {
	prInfo := getPRInformation(event)
	if prInfo.repoName != "vitess" {
		return nil
	}

	err := h.createDocsPreview(ctx, event, prInfo)
	if err != nil {
		return err
	}
	err = h.createErrorDocumentation(ctx, event, prInfo)
	if err != nil {
		return err
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

	vitess := git.NewRepo(
		prInfo.repoOwner,
		prInfo.repoName,
	).WithLocalDir(filepath.Join(h.Workdir(), "vitess"))

	logger.Debug().Msgf("Listing changed files in Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	changeDetected, err := detectErrorCodeChanges(ctx, vitess, prInfo, client)
	if err != nil {
		logger.Err(err).Msg(err.Error())
		return nil
	}
	if !changeDetected {
		logger.Debug().Msgf("No change detect to 'go/vt/vterrors/code.go' in Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
		return nil
	}
	logger.Debug().Msgf("Change detect to 'go/vt/vterrors/code.go' in Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)

	h.vitessRepoLock.Lock()
	vterrorsgenVitess, err := cloneVitessAndGenerateErrors(ctx, vitess, prInfo)
	h.vitessRepoLock.Unlock()
	if err != nil {
		logger.Err(err).Msg(err.Error())
		return nil
	}

	website := git.NewRepo(
		prInfo.repoOwner,
		"website",
	).WithLocalDir(filepath.Join(h.Workdir(), "website"))

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

func (h *PullRequestHandler) addArewefastyetComment(ctx context.Context, event github.PullRequestEvent, prInfo prInformation) (err error) {
	if event.GetLabel().GetName() != "Benchmark me" {
		return nil
	}

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

	newComment := fmt.Sprintf("Hello! :wave:\n\nThis Pull Request is now handled by arewefastyet. The current HEAD and future commits will be benchmarked.\n\nYou can find the performance comparison on the [arewefastyet website](https://benchmark.vitess.io/pr/%d).", prInfo.num)

	// use client to get comments
	var allComments []*github.IssueComment
	perPage := 100
	for page := 1; true; page++ {
		comments, _, err := client.Issues.ListComments(ctx, prInfo.repoOwner, prInfo.repoName, prInfo.num, &github.IssueListCommentsOptions{
			ListOptions: github.ListOptions{
				Page:    page,
				PerPage: perPage,
			},
		})
		if err != nil {
			logger.Error().Err(err).Msgf("failed to get comments on Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
			return err
		}
		allComments = append(allComments, comments...)
		if len(comments) < perPage {
			break
		}
	}

	// look through comments
	for _, comment := range allComments {
		body := comment.GetBody()
		if strings.Contains(body, newComment) {
			logger.Info().Msgf("arewefastyet comment already added to Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
			return nil
		}
	}

	prComment := github.IssueComment{
		Body: &newComment,
	}

	logger.Debug().Msgf("Adding arewefastyet comment to Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	if _, _, err := client.Issues.CreateComment(ctx, prInfo.repoOwner, prInfo.repoName, prInfo.num, &prComment); err != nil {
		logger.Error().Err(err).Msgf("Failed to add arewefastyet comment to Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
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

	vitessRepo := git.NewRepo(
		prInfo.repoOwner,
		prInfo.repoName,
	).WithLocalDir(filepath.Join(h.Workdir(), "vitess"))
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

var releaseBranchRegexp = regexp.MustCompile(`release-(\d+\.\d+)`)

func (h *PullRequestHandler) createDocsPreview(ctx context.Context, event github.PullRequestEvent, prInfo prInformation) error {
	// Checks:
	// 1. Is a PR against either:
	// 	- vitessio/vitess:main
	//	- vitessio/vitess:release-\d+\.\d+
	// 2. PR contains changes to either `go/cmd/**/*.go` OR `go/flags/endtoend/*.txt`
	if prInfo.base.GetRef() == "main" {
		return h.previewCobraDocs(ctx, event, "main", prInfo)
	} else if m := releaseBranchRegexp.FindStringSubmatch(prInfo.base.GetRef()); m != nil {
		return h.previewCobraDocs(ctx, event, m[1], prInfo)
	}

	return nil
}

func (h *PullRequestHandler) previewCobraDocs(ctx context.Context, event github.PullRequestEvent, docsVersion string, prInfo prInformation) error {
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

	vitess := git.NewRepo(
		prInfo.repoOwner,
		prInfo.repoName,
	).WithLocalDir(filepath.Join(h.Workdir(), "vitess"))

	docChanges, err := detectCobraDocChanges(ctx, vitess, client, prInfo)
	if err != nil {
		return err
	}

	if !docChanges {
		logger.Debug().Msgf("No flags changes detected in Pull Request %s/%s#%d", vitess.Owner, vitess.Name, prInfo.num)
		return nil
	}

	website := git.NewRepo(
		prInfo.repoOwner,
		"website",
	).WithDefaultBranch("prod").WithLocalDir(
		filepath.Join(h.Workdir(), "website"),
	)

	_, err = h.createCobraDocsPreviewPR(ctx, client, vitess, website, event.GetPullRequest(), docsVersion, prInfo)
	return err
}

func (h *PullRequestHandler) createCobraDocsPreviewPR(
	ctx context.Context,
	client *github.Client,
	vitess *git.Repo,
	website *git.Repo,
	pr *github.PullRequest,
	docsVersion string,
	prInfo prInformation,
) (*github.PullRequest, error) {
	logger := zerolog.Ctx(ctx)
	// 1. Find an existing PR and switch to its branch, or create a new branch
	// based on `prod`.
	branch := "prod"
	headBranch := cobraDocsSyncBranchName(prInfo.num)
	headRef := fmt.Sprintf("refs/heads/%s", headBranch)

	prodBranch, _, err := client.Repositories.GetBranch(ctx, website.Owner, website.Name, branch, false)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed get production branch on %s/%s to preview cobradocs on Pull Request %d", website.Owner, website.Name, pr.GetNumber())
	}

	baseTree := prodBranch.GetCommit().Commit.Tree.GetSHA()
	parent := prodBranch.GetCommit().GetSHA()
	op := "generate cobradocs preview"
	var openPR *github.PullRequest

	if err := createAndCheckoutBranch(ctx, client, website, branch, headBranch, fmt.Sprintf("%s for %s", op, pr.GetHTMLURL())); err != nil {
		return nil, err
	}

	if docsVersion == "main" {
		// We need to replace "main" with whatever the highest version under
		// content/en/docs is.
		//
		// MacOS does not support -regextype flag, but Unix (non-darwin) does
		// not support the -E equivalent. Similarly, Unix does not support the
		// lexicographic sort opt (-s), so we rely on sort's dictionary sort
		// (-d) instead.
		//
		// Unix version: find content/en/docs -regextype posix-extended -maxdepth 1 -type d -regex ... | sort -d
		// MacOS version: find -E content/en/docs -maxdepth 1 -type d -regex ... | sort -d
		args := shell.FindRegexpExtended("content/en/docs",
			"-maxdepth", "1",
			"-type", "d",
			"-regex", `.*/[0-9]+.[0-9]+`, "|",
			"sort", "-d",
		)
		find, err := shell.NewContext(ctx, "bash", "-c", strings.Join(args, " ")).InDir(website.LocalDir).Output()
		if err != nil {
			return nil, errors.Wrapf(err, "Failed to `find` latest docs version to %s for %s", op, pr.GetHTMLURL())
		}

		lines := strings.Split(string(find), "\n")
		if len(lines) == 0 {
			return nil, errors.Errorf("Failed to `find` any doc versions: found %s", string(find))
		}

		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}

		docsVersion = filepath.Base(lines[len(lines)-1])
		logger.Debug().Msgf("Found latest version of docs to be %s", docsVersion)
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

		// 1a. If branch already existed, hard reset to `prod`.
		if err := website.ResetHard(ctx, branch); err != nil {
			return nil, errors.Wrapf(err, "Failed to reset %s to %s to %s for %s", headBranch, branch, op, pr.GetHTMLURL())
		}
	}

	// 2. Clone vitess and switch to the PR's base ref.
	if err := setupRepo(ctx, vitess, fmt.Sprintf("%s for %s", op, pr.GetHTMLURL())); err != nil {
		return nil, err
	}

	remote := pr.GetBase().GetRepo().GetCloneURL()
	ref := pr.GetBase().GetRef()
	if err := vitess.FetchRef(ctx, remote, ref); err != nil {
		return nil, errors.Wrapf(err, "Failed to fetch %s:%s to %s for %s", remote, ref, op, pr.GetHTMLURL())
	}

	if err := vitess.Checkout(ctx, "FETCH_HEAD"); err != nil {
		return nil, errors.Wrapf(err, "Failed to checkout %s:%s to %s for %s", remote, ref, op, pr.GetHTMLURL())
	}

	var (
		tree            *github.Tree
		commit          *github.Commit
		skipFirstCommit bool
	)

	// 3. Run the sync script with `COBRADOC_VERSION_PAIRS="$(baseref):$(docsVersion)`.
	_, err = shell.NewContext(ctx, "./tools/sync_cobradocs.sh").InDir(website.LocalDir).WithExtraEnv(
		fmt.Sprintf("VITESS_DIR=%s", vitess.LocalDir),
		"COBRADOCS_SYNC_PERSIST=yes",
		fmt.Sprintf("COBRADOC_VERSION_PAIRS=HEAD:%s", docsVersion),
	).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) &&
			bytes.Contains(exitErr.Stderr, []byte("No changes to cobradocs detected")) {
			logger.Info().Msgf("No cobradocs changed for PR %s/%s#%d at base %s. Skipping first commit ...", remote, vitess.Name, pr.GetNumber(), ref)
			skipFirstCommit = true
		} else {
			logger.Err(err).Msgf("%T", err)
			return nil, errors.Wrapf(err, "Failed to run cobradocs sync script against %s:%s to %s for %s", remote, ref, op, pr.GetHTMLURL())

		}
	}

	if !skipFirstCommit {
		tree, commit, err = h.writeAndCommitTree(
			ctx,
			client,
			website,
			pr,
			branch,
			"HEAD",
			baseTree,
			parent,
			fmt.Sprintf("Generate cobradocs preview against %s:%s", remote, ref),
			op,
		)
		if err != nil {
			return nil, err
		}
	} else {
		tree = &github.Tree{
			SHA: &baseTree,
		}
		commit = &github.Commit{
			SHA: &parent,
		}
	}

	// 4. Switch vitess repo to the PR's head ref.
	if err := vitess.FetchRef(ctx, remote, fmt.Sprintf("refs/pull/%d/head", pr.GetNumber())); err != nil {
		return nil, errors.Wrapf(err, "Failed to fetch Pull Request %s/%s#%d to %s for %s", vitess.Owner, vitess.Name, pr.GetNumber(), op, pr.GetHTMLURL())
	}
	if err := vitess.Checkout(ctx, "FETCH_HEAD"); err != nil {
		return nil, errors.Wrapf(err, "Failed to checkout %s in %s/%s to %s for %s", ref, vitess.Owner, vitess.Name, op, pr.GetHTMLURL())
	}

	// 5. Run the sync script again with `COBRADOC_VERSION_PAIRS=$(headref):$(docsVersion)`.
	_, err = shell.NewContext(ctx, "./tools/sync_cobradocs.sh").InDir(website.LocalDir).WithExtraEnv(
		fmt.Sprintf("VITESS_DIR=%s", vitess.LocalDir),
		"COBRADOCS_SYNC_PERSIST=yes",
		fmt.Sprintf("COBRADOC_VERSION_PAIRS=HEAD:%s", docsVersion),
	).Output()
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to run cobradocs sync script against %s/%s:%s to %s for %s", vitess.Owner, vitess.Name, ref, op, pr.GetHTMLURL())
	}

	_, commit, err = h.writeAndCommitTree(
		ctx,
		client,
		website,
		pr,
		"HEAD~1",
		"HEAD",
		tree.GetSHA(),
		commit.GetSHA(),
		fmt.Sprintf(
			"Generate cobradocs preview against %s/%s:%s",
			pr.GetHead().GetRepo().GetOwner().GetLogin(),
			pr.GetHead().GetRepo().GetName(),
			pr.GetHead().GetRef(),
		),
		op,
	)
	if err != nil {
		return nil, err
	}

	// 6. Force push.
	if _, _, err := client.Git.UpdateRef(ctx, website.Owner, website.Name, &github.Reference{
		Ref:    &headRef,
		Object: &github.GitObject{SHA: commit.SHA},
	}, true); err != nil {
		return nil, errors.Wrapf(err, "Failed to force-push %s to %s on Pull Request %s", headBranch, op, pr.GetHTMLURL())
	}

	switch openPR {
	case nil:
		// 7. Create PR with clear instructions that this is for preview purposes only
		// and must not be merged.
		newPR := &github.NewPullRequest{
			Title:               github.String(fmt.Sprintf("[DO NOT MERGE] [cobradocs] preview cobradocs changes for %s/%s#%d", vitess.Owner, vitess.Name, prInfo.num)),
			Head:                github.String(headBranch),
			Base:                github.String(branch),
			Body:                github.String(fmt.Sprintf("## Description\nThis is an automated PR to preview changes to the the released cobradocs with %s", pr.GetHTMLURL())),
			MaintainerCanModify: github.Bool(true),
		}
		openPR, _, err = client.PullRequests.Create(ctx, website.Owner, website.Name, newPR)
		if err != nil {
			return nil, errors.Wrapf(err, "Failed to create Pull Request using branch %s on %s/%s", headBranch, website.Owner, website.Name)
		}
	default:
		// 7a. In case of branch/PR already existing, add a comment saying that the
		// vitess PR was updated so we force pushed to re-sync the preview changes.
		if _, _, err := client.Issues.CreateComment(ctx, website.Owner, website.Name, openPR.GetNumber(), &github.IssueComment{
			Body: github.String(fmt.Sprintf("This preview-only PR was force-pushed to resync changes in vitess PR %s", pr.GetHTMLURL())),
		}); err != nil {
			return nil, errors.Wrapf(err, "Failed to add PR comment on %s", openPR.GetHTMLURL())
		}
	}

	// 8. In either case, make sure a do-not-merge label is on the website PR.
	if _, _, err = client.Issues.AddLabelsToIssue(ctx, website.Owner, website.Name, openPR.GetNumber(), []string{doNotMergeLabel}); err != nil {
		return nil, errors.Wrapf(err, "Failed to add %s label to %s", doNotMergeLabel, openPR.GetHTMLURL())
	}

	return openPR, nil
}

func (h *PullRequestHandler) writeAndCommitTree(
	ctx context.Context,
	client *github.Client,
	repo *git.Repo,
	pr *github.PullRequest,
	baseRef string,
	headRef string,
	baseTree string,
	parentCommit string,
	commitMsg string,
	op string,
) (*github.Tree, *github.Commit, error) {
	out, err := repo.DiffTree(ctx, baseRef, headRef, git.DiffTreeOpts{Recursive: true})
	if err != nil {
		return nil, nil, errors.Wrapf(err, "Failed to diff-tree %s %s in %s/%s to %s for %s", baseRef, headRef, repo.Owner, repo.Name, op, pr.GetHTMLURL())
	}

	lines := bytes.Split(out, []byte{'\n'})

	var tree = &github.Tree{}

	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		entry, err := git.ParseDiffTreeEntry(string(line), repo.LocalDir)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "Failed to parse diff-tree entry to %s for %s", op, pr.GetHTMLURL())
		}

		tree.Entries = append(tree.Entries, entry)
	}

	tree, _, err = client.Git.CreateTree(ctx, repo.Owner, repo.Name, baseTree, tree.Entries)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "Failed to create tree based on %s to %s for %s", baseTree, op, pr.GetHTMLURL())
	}

	commit := &github.Commit{
		Message: &commitMsg,
		Tree:    tree,
		Parents: []*github.Commit{
			{SHA: &parentCommit},
		},
	}

	commit, _, err = client.Git.CreateCommit(ctx, repo.Owner, repo.Name, commit)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "Failed to create commit based on %s to %s for %s", parentCommit, op, pr.GetHTMLURL())
	}

	return tree, commit, nil
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

	website := git.NewRepo(
		prInfo.repoOwner,
		"website",
	).WithDefaultBranch("prod").WithLocalDir(
		filepath.Join(h.Workdir(), "website"),
	)

	// Checks:
	// - is vitessio/vitess:main branch
	// - PR contains changes to either `go/cmd/**/*.go` OR `go/flags/endtoend/*.txt` (TODO)
	if prInfo.base.GetRef() != "main" {
		logger.Debug().Msgf("PR %d is merged to %s, not main, skipping website cobradocs sync", prInfo.num, prInfo.base.GetRef())
		// Close any potentially open PR against website.
		// (see https://github.com/vitessio/vitess-bot/issues/76).
		prs, err := website.FindPRs(ctx, client, github.PullRequestListOptions{
			State:     "open",
			Head:      fmt.Sprintf("%s:%s", website.Owner, cobraDocsSyncBranchName(prInfo.num)),
			Base:      website.DefaultBranch,
			Sort:      "created",
			Direction: "desc",
		}, func(pr *github.PullRequest) bool {
			return pr.GetUser().GetLogin() == h.botLogin
		}, 1)
		if err != nil {
			return err
		}

		if len(prs) == 0 {
			// No open PRs.
			return nil
		}

		openPR := prs[0]
		logger.Info().Msgf("closing open PR %s/%s#%d", website.Owner, website.Name, openPR.GetNumber())
		_, _, err = client.PullRequests.Edit(ctx, website.Owner, website.Name, openPR.GetNumber(), &github.PullRequest{
			State: github.String("closed"),
		})
		if err != nil {
			return errors.Wrapf(err, "Failed to close PR %s/%s#%d", website.Owner, website.Name, openPR.GetNumber())
		}
		return nil
	}

	vitess := git.NewRepo(
		prInfo.repoOwner,
		prInfo.repoName,
	).WithLocalDir(filepath.Join(h.Workdir(), "vitess"))

	docChanges, err := detectCobraDocChanges(ctx, vitess, client, prInfo)
	if err != nil {
		return err
	}

	if !docChanges {
		logger.Debug().Msgf("No flags changes detected in Pull Request %s/%s#%d", vitess.Owner, vitess.Name, prInfo.num)
		return nil
	}

	pr, err := h.synchronizeCobraDocs(ctx, client, vitess, website, event.GetPullRequest(), prInfo)
	if err != nil {
		return err
	}

	if pr != nil {
		_, _, err = client.PullRequests.Merge(
			ctx,
			website.Owner,
			website.Name,
			pr.GetNumber(),
			"", // Default to the standard automatic commit message.
			&github.PullRequestOptions{
				SHA:         pr.GetHead().GetSHA(), // Fail if the branch has changed out from under us.
				MergeMethod: "squash",
			},
		)

		if err != nil {
			return errors.Wrapf(err, "Failed to merge Pull Request %s", pr.GetHTMLURL())
		}
	}

	return nil
}

func detectCobraDocChanges(ctx context.Context, vitess *git.Repo, client *github.Client, prInfo prInformation) (bool, error) {
	files, err := vitess.ListPRFiles(ctx, client, prInfo.num)
	if err != nil {
		return false, err
	}

	for _, file := range files {
		if strings.HasPrefix(file.GetFilename(), "go/cmd") && strings.HasSuffix(file.GetFilename(), ".go") {
			return true, nil
		}

		if strings.HasPrefix(file.GetFilename(), "go/flags/endtoend/") && strings.HasSuffix(file.GetFilename(), ".txt") {
			return true, nil
		}
	}

	return false, nil
}
