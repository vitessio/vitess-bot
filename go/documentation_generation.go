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
	"os"
	"strings"

	"github.com/google/go-github/v53/github"
	"github.com/pkg/errors"
	"github.com/vitess.io/vitess-bot/go/git"
	"github.com/vitess.io/vitess-bot/go/shell"
)

const (
	errorCodePrefixLabel = "<!-- start -->"
	errorCodeSuffixLabel = "<!-- end -->"

	rowsPerPage = 100
)

func detectErrorCodeChanges(ctx context.Context, prInfo prInformation, client *github.Client) (bool, error) {
	var allFiles []*github.CommitFile
	cont := true
	for page := 1; cont; page++ {
		files, _, err := client.PullRequests.ListFiles(ctx, prInfo.repoOwner, prInfo.repoName, prInfo.num, &github.ListOptions{PerPage: rowsPerPage})
		if err != nil {
			return false, errors.Wrapf(err, "Failed to list changed files in Pull Request %s/%s#%d - at page %d", prInfo.repoOwner, prInfo.repoName, prInfo.num, page)
		}
		allFiles = append(allFiles, files...)
		if len(files) < rowsPerPage {
			cont = false
			break
		}
	}
	for _, file := range allFiles {
		if file.GetFilename() == "go/vt/vterrors/code.go" {
			return true, nil
		}
	}
	return false, nil
}

func cloneVitessAndGenerateErrors(ctx context.Context, vitess *git.Repo, prInfo prInformation) (string, error) {
	if err := vitess.Clone(ctx); err != nil {
		return "", errors.Wrapf(err, "Failed to clone repository %s/%s to generate error code on Pull Request %d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	}

	// Clean the repository
	if err := vitess.Clean(ctx); err != nil {
		return "", errors.Wrapf(err, "Failed to clean the repository %s/%s to generate documentation %d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	}

	if err := vitess.FetchRef(ctx, "origin", fmt.Sprintf("refs/pull/%d/head", prInfo.num)); err != nil {
		return "", errors.Wrapf(err, "Failed to fetch Pull Request %s/%s#%d to generate error code", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	}

	if err := vitess.Checkout(ctx, "FETCH_HEAD"); err != nil {
		return "", errors.Wrapf(err, "Failed to checkout on Pull Request %s/%s#%d to generate error code", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	}

	vterrorsgenVitessBytes, err := shell.NewContext(ctx, "go", "run", "./go/vt/vterrors/vterrorsgen").InDir("/tmp/vitess").Output()
	if err != nil {
		return "", errors.Wrapf(err, "Failed to run ./go/vt/vterrors/vterrorsgen on Pull Request %s/%s#%d to generate error code", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	}
	return string(vterrorsgenVitessBytes), err
}

func cloneWebsiteAndGetCurrentVersionOfDocs(ctx context.Context, website *git.Repo, prInfo prInformation) (string, error) {
	if err := website.Clone(ctx); err != nil {
		return "", errors.Wrapf(err, "Failed to clone repository vitessio/website to generate error code on Pull Request %d", prInfo.num)
	}

	if err := website.Clean(ctx); err != nil {
		return "", errors.Wrapf(err, "Failed to fetch vitessio/website to generate error code on Pull Request %d", prInfo.num)
	}

	_, err := shell.NewContext(ctx, "cp", "./tools/get_release_from_docs.sh", "/tmp/website").Output()
	if err != nil {
		return "", errors.Wrapf(err, "Failed to copy ./tools/get_release_from_docs.sh to local clone of website repo to generate error code on Pull Request %d", prInfo.num)
	}

	currentVersionDocsBytes, err := shell.New("./get_release_from_docs.sh").InDir("/tmp/website").Output()
	if err != nil {
		return "", errors.Wrapf(err, "Failed to get current documentation version from config.toml in vitessio/website to generate error code on Pull Request %d", prInfo.num)
	}
	return string(currentVersionDocsBytes), nil
}

func generateErrorCodeDocumentation(
	ctx context.Context,
	client *github.Client,
	website *git.Repo,
	prInfo prInformation,
	currentVersionDocs, vterrorsgenVitess string,
) (string, string, error) {
	prDetails, _, err := client.PullRequests.Get(ctx, prInfo.repoOwner, prInfo.repoName, prInfo.num)
	if err != nil {
		return "", "", errors.Wrapf(err, "Failed to get the details of Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	}

	base := prDetails.GetBase()
	if base == nil {
		return "", "", errors.Wrapf(err, "Could not find the base of Pull Request %s/%s#%d", prInfo.repoOwner, prInfo.repoName, prInfo.num)
	}
	if strings.HasPrefix(base.GetRef(), "release-") && len(base.GetRef()) == len("release-00.0") {
		currentVersionDocs = strings.Split(base.GetRef(), "-")[1]
	}

	docPath := "/tmp/website/content/en/docs/" + currentVersionDocs + "/reference/errors/query-serving.md"
	queryServingErrorsBytes, err := shell.NewContext(ctx, "cat", docPath).InDir("/tmp/website").Output()
	if err != nil {
		return "", "", errors.Wrapf(err, "Failed to cat the query serving error file (%s) to generate error code for Pull Request %d", docPath, prInfo.num)
	}
	queryServingErrors := string(queryServingErrorsBytes)

	startIdx := strings.Index(queryServingErrors, errorCodePrefixLabel) + len(errorCodePrefixLabel)
	endIdx := strings.Index(queryServingErrors, errorCodeSuffixLabel)
	newQueryServingError := strings.Replace(queryServingErrors, queryServingErrors[startIdx:endIdx], fmt.Sprintf("\n%s", vterrorsgenVitess), 1)

	err = os.WriteFile(docPath, []byte(newQueryServingError), os.ModePerm)
	if err != nil {
		return "", "", errors.Wrapf(err, "Cannot write file (%s) to generate errors of Pull Request %d", docPath, prInfo.num)
	}

	statusBytes, err := website.Status(ctx, "-s")
	if err != nil {
		return "", "", errors.Wrapf(err, "Failed to do git status on vitessio/website to generate error code on Pull Request %d", prInfo.num)
	}
	if len(statusBytes) == 0 {
		return "", "", nil
	}

	errorDocContentBytes, err := os.ReadFile(docPath)
	if err != nil {
		return "", "", errors.Wrapf(err, "Failed read final documentation file for error code generation on Pull Request %d", prInfo.num)
	}
	return string(errorDocContentBytes), docPath, nil
}

func createCommitAndPullRequestForErrorCode(
	ctx context.Context,
	prInfo prInformation,
	client *github.Client,
	errorDocContent, docPath string,
) error {
	baseTree := ""
	parent := ""
	newBranch := false
	branchName := fmt.Sprintf("update-error-code-%d", prInfo.num)
	refName := "refs/heads/" + branchName
	branch, r, err := client.Repositories.GetBranch(ctx, prInfo.repoOwner, "website", branchName, false)
	if r.StatusCode != http.StatusNotFound && err != nil {
		return errors.Wrapf(err, "Failed to get branch on vitessio/website to generate error code on Pull Request %d", prInfo.num)
	}

	// If the branchName is not a branch on the repository, we will receive a http.StatusNotFound status code
	// we then create the branch. Otherwise, we use the already existing branchName.
	if r.StatusCode == http.StatusNotFound {
		newBranch = true

		prodBranch, _, err := client.Repositories.GetBranch(ctx, prInfo.repoOwner, "website", "prod", false)
		if err != nil {
			return errors.Wrapf(err, "Failed get production branch on vitessio/website to generate error code on Pull Request %d", prInfo.num)
		}

		baseTree = prodBranch.GetCommit().Commit.Tree.GetSHA()
		parent = prodBranch.GetCommit().GetSHA()

		_, _, err = client.Git.CreateRef(ctx, prInfo.repoOwner, "website", &github.Reference{
			Ref: &refName,
			Object: &github.GitObject{
				SHA: &parent,
			},
		})
		if err != nil {
			return errors.Wrapf(err, "Failed to create git ref on vitessio/website to generate error code on Pull Request %d", prInfo.num)
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
		return errors.Wrapf(err, "Failed create blob to generate error code on Pull Request %d", prInfo.num)
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
		return errors.Wrapf(err, "Failed create tree to generate error code on Pull Request %d", prInfo.num)
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
		return errors.Wrapf(err, "Failed create commit to generate error code on Pull Request %d", prInfo.num)
	}

	// Update a reference
	ref := &github.Reference{
		Ref:    github.String(refName),
		Object: &github.GitObject{SHA: commit.SHA},
	}
	_, _, err = client.Git.UpdateRef(ctx, prInfo.repoOwner, "website", ref, true)
	if err != nil {
		return errors.Wrapf(err, "Failed to update ref to generate error code on Pull Request %d", prInfo.num)
	}

	// Create a PR if needed
	if newBranch {
		newPR := &github.NewPullRequest{
			Title:               github.String(fmt.Sprintf("Update error code documentation (#%d)", prInfo.num)),
			Head:                github.String(branchName),
			Base:                github.String("prod"),
			Body:                github.String(fmt.Sprintf("## Description\nThis Pull Request updates the error code documentation based on the changes made in https://github.com/%s/vitess/pull/%d", prInfo.repoOwner, prInfo.num)),
			MaintainerCanModify: github.Bool(true),
		}
		_, _, err = client.PullRequests.Create(ctx, prInfo.repoOwner, "website", newPR)
		if err != nil {
			return errors.Wrapf(err, "Failed create PR to generate error code on Pull Request %d", prInfo.num)
		}
	}
	return nil
}
