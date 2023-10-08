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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/google/go-github/v53/github"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/vitess.io/vitess-bot/go/git"
	"github.com/vitess.io/vitess-bot/go/semver"
	"github.com/vitess.io/vitess-bot/go/shell"
)

type releaseMetadata struct {
	repoName  string
	repoOwner string

	tag        string
	draft      bool
	prerelease bool

	url string
}

func getReleaseMetadata(event *github.ReleaseEvent) *releaseMetadata {
	return &releaseMetadata{
		repoOwner:  event.GetRepo().GetOwner().GetLogin(),
		repoName:   event.GetRepo().GetName(),
		tag:        event.GetRelease().GetTagName(),
		draft:      event.GetRelease().GetDraft(),
		prerelease: event.GetRelease().GetPrerelease(),
		url:        event.GetRelease().GetHTMLURL(),
	}
}

type ReleaseHandler struct {
	githubapp.ClientCreator
	botLogin string

	m sync.Mutex
}

func NewReleaseHandler(cc githubapp.ClientCreator, botLogin string) (h *ReleaseHandler, err error) {
	h = &ReleaseHandler{
		ClientCreator: cc,
		botLogin:      botLogin,
	}
	err = os.MkdirAll(h.Workdir(), 0777|os.ModeDir)

	return h, err
}

func (h *ReleaseHandler) Workdir() string {
	return filepath.Join("/", "tmp", "release_handler")
}

func (h *ReleaseHandler) Handles() []string {
	return []string{"release"}
}

func (h *ReleaseHandler) Handle(ctx context.Context, _, _ string, payload []byte) error {
	var event github.ReleaseEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.Wrap(err, "Failed to parse release event payload")
	}

	switch event.GetAction() {
	case "published":
		releaseMeta := getReleaseMetadata(&event)
		if releaseMeta.repoName != "vitess" {
			return nil
		}

		if releaseMeta.draft {
			return nil
		}

		version, err := semver.Parse(releaseMeta.tag)
		if err != nil { // release tag is not semver-compliant (which includes release candidates)
			return nil
		}

		client, err := h.NewInstallationClient(githubapp.GetInstallationIDFromEvent(&event))
		if err != nil {
			return err
		}

		h.m.Lock()
		defer h.m.Unlock()

		_, err = h.updateReleasedCobraDocs(ctx, client, releaseMeta, version)
		if err != nil {
			return err
		}

		return nil
	}

	return nil
}

// TODO: refactor out shared code between here and synchronizeCobraDocs()
func (h *ReleaseHandler) updateReleasedCobraDocs(
	ctx context.Context,
	client *github.Client,
	releaseMeta *releaseMetadata,
	version semver.Version,
) (*github.PullRequest, error) {
	vitess := git.NewRepo(
		releaseMeta.repoOwner,
		"vitess",
	).WithLocalDir(filepath.Join(h.Workdir(), "vitess"))
	website := git.NewRepo(
		releaseMeta.repoOwner,
		"website",
	).WithLocalDir(filepath.Join(h.Workdir(), "website"))

	prs, err := website.ListPRs(ctx, client, github.PullRequestListOptions{
		State:     "open",
		Head:      "update-release-cobradocs-for-",
		Base:      "prod",
		Sort:      "created",
		Direction: "desc",
	})
	if err != nil {
		return nil, err
	}

	logger := zerolog.Ctx(ctx)
	branch := "prod"
	newBranch := fmt.Sprintf("update-release-cobradocs-for-%s", version.String())
	op := "update release cobradocs"

	for _, pr := range prs {
		if pr.GetUser().GetLogin() != h.botLogin {
			continue
		}

		// Most recent PR created by the bot. Base a new PR off of it.
		head := pr.GetHead()

		branch = head.GetRef()
		repo := head.GetRepo()

		logger.Debug().Msgf("using existing PR #%d (%s/%s:%s)", pr.GetNumber(), repo.GetOwner(), repo.GetName(), branch)
		break
	}

	if err := createAndCheckoutBranch(ctx, client, website, branch, newBranch, fmt.Sprintf("%s for %s", op, version.String())); err != nil {
		return nil, err
	}

	if err := setupRepo(ctx, vitess, fmt.Sprintf("%s for %s", op, version.String())); err != nil {
		return nil, err
	}

	if err := vitess.FetchRef(ctx, "origin", "--tags"); err != nil {
		return nil, errors.Wrapf(err, "Failed to fetch tags in repository %s/%s to %s for %s", vitess.Owner, vitess.Name, op, version.String())
	}

	awk, err := shell.NewContext(ctx,
		"awk",
		"-F\"",
		"-e",
		`$0 ~ /COBRADOC_VERSION_PAIRS="?([^"])"?/ { printf $2 }`,
		"Makefile",
	).InDir(website.LocalDir).Output()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to extract COBRADOC_VERSION_PAIRS from website Makefile")
	}

	versionPairs, err := extractVersionPairsFromWebsite(string(awk))
	if err != nil {
		return nil, errors.Wrap(err, "Failed to extract COBRADOC_VERSION_PAIRS from website Makefile")
	}

	versionPairs = updateVersionPairs(versionPairs, version)

	// Update the Makefile and author a commit.
	if err := replaceVersionPairs(ctx, website, versionPairs); err != nil {
		return nil, errors.Wrapf(err, "Failed to update COBRADOC_VERSION_PAIRS in repository %s/%s to %s for %s", website.Owner, website.Name, op, version.String())
	}

	if err := website.Add(ctx, "Makefile"); err != nil {
		return nil, errors.Wrapf(err, "Failed to stage changes in repository %s/%s to %s for %s", website.Owner, website.Name, op, version.String())
	}

	if err := website.Commit(ctx, fmt.Sprintf("Update COBRADOC_VERSION_PAIRS for new release %s", version.String()), git.CommitOpts{
		Author: botCommitAuthor,
	}); err != nil {
		return nil, errors.Wrapf(err, "Failed to commit COBRADOC_VERSION_PAIRS in repository %s/%s to %s for %s", website.Owner, website.Name, op, version.String())
	}

	// Run the sync script (which authors the commit already).
	_, err = shell.NewContext(ctx, "./tools/sync_cobradocs.sh").InDir(website.LocalDir).WithExtraEnv(
		fmt.Sprintf("VITESS_DIR=%s", vitess.LocalDir),
		"COBRADOCS_SYNC_PERSIST=yes",
	).Output()
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to run cobradoc sync script in repository %s/%s to %s for %s", website.Owner, website.Name, op, version.String())
	}

	// Amend the commit to change the author to the bot, and change the message
	// to something more appropriate.
	if err := website.Commit(ctx, fmt.Sprintf("Update released cobradocs with %s", releaseMeta.url), git.CommitOpts{
		Author: botCommitAuthor,
		Amend:  true,
	}); err != nil {
		return nil, errors.Wrapf(err, "Failed to amend commit author to %s for %s", op, version.String())
	}

	// Push the branch
	if err := website.Push(ctx, git.PushOpts{
		Remote: "origin",
		Refs:   []string{newBranch},
		Force:  true,
	}); err != nil {
		return nil, errors.Wrapf(err, "Failed to push %s to %s for %s", newBranch, op, version.String())
	}

	// Create a Pull Request for the new branch.
	newPR := &github.NewPullRequest{
		Title:               github.String(fmt.Sprintf("[cobradocs] update released cobradocs with %s", version.String())),
		Head:                github.String(newBranch),
		Base:                github.String("prod"), // hard-coded since sometimes `branch` is a different base.
		Body:                github.String(fmt.Sprintf("## Description\nThis is an automated PR to update the released cobradocs with [%s](%s)", version.String(), releaseMeta.url)),
		MaintainerCanModify: github.Bool(true),
	}
	newPRCreated, _, err := client.PullRequests.Create(ctx, website.Owner, website.Name, newPR)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to create Pull Request using branch %s on %s/%s", newBranch, website.Owner, website.Name)
	}

	return newPRCreated, nil
}

type versionPair struct {
	release semver.Version
	tag     string
	docs    string
}

// For example:
// export COBRADOC_VERSION_PAIRS="main:19.0,v18.0.0-rc1:18.0,v17.0.3:17.0,v16.0.5:16.0,v15.0.5:15.0"
func extractVersionPairsFromWebsite(awk string) (versions []*versionPair, err error) {
	if len(awk) == 0 {
		return nil, errors.New("no version pair data from website")
	}

	for _, pair := range strings.Split(awk, ",") {
		parts := strings.Split(pair, ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("bad version pair %s", pair)
		}

		var vp versionPair
		switch parts[0] {
		case "main": // special handling for the main branch
			vp.tag = parts[0]
		default:
			vp.release, err = semver.Parse(parts[0])
			if err != nil {
				return nil, err
			}
		}

		vp.docs = parts[1]
		versions = append(versions, &vp)
	}

	return versions, nil
}

func updateVersionPairs(originalPairs []*versionPair, version semver.Version) (newPairs []*versionPair) {
	var isRCBump bool
	for _, pair := range originalPairs {
		if version.RCVersion == 0 {
			break
		}

		if pair.release.Major == version.Major {
			isRCBump = true
			break
		}
	}

	newPairs = make([]*versionPair, 0, len(originalPairs))
	// Find the pair we need to update in the Makefile.
	for _, pair := range originalPairs {
		switch {
		case pair.release.Major == version.Major:
			newPairs = append(newPairs, &versionPair{
				release: version,
				docs:    pair.docs,
			})
		case pair.tag == "main" && version.RCVersion > 0 && !isRCBump:
			// Insert new version for "main:<version.Major+1>"
			newPairs = append([]*versionPair{{
				tag:  "main",
				docs: fmt.Sprintf("%d.0", version.Major+1),
			}}, newPairs...)
			newPairs = append(newPairs, &versionPair{
				release: version,
				docs:    pair.docs,
			})
		default:
			newPairs = append(newPairs, pair)
		}
	}

	return newPairs
}

func replaceVersionPairs(ctx context.Context, website *git.Repo, versionPairs []*versionPair) error {
	slices.SortFunc(versionPairs, func(a, b *versionPair) int {
		return -strings.Compare(a.docs, b.docs)
	})

	var (
		buf   strings.Builder
		pairs []string
	)
	for _, pair := range versionPairs {
		if pair.tag != "" {
			buf.WriteString(pair.tag)
		} else {
			fmt.Fprintf(&buf, "v%s", pair.release.String())
		}

		buf.WriteString(":")
		buf.WriteString(pair.docs)

		pairs = append(pairs, buf.String())
		buf.Reset()
	}

	_, err := shell.NewContext(ctx,
		"sed",
		"-i", "",
		"-e", fmt.Sprintf(`s/\(export COBRADOC_VERSION_PAIRS=\).*/\1%q/`, strings.Join(pairs, ",")),
		"Makefile",
	).InDir(website.LocalDir).Output()
	return err
}
