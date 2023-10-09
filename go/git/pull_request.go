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

	"github.com/google/go-github/v53/github"
	"github.com/pkg/errors"
)

const rowsPerPage = 100

func (r *Repo) ListPRs(ctx context.Context, client *github.Client, opts github.PullRequestListOptions) (pulls []*github.PullRequest, err error) {
	cont := true
	for page := 1; cont; page++ {
		opts.ListOptions = github.ListOptions{
			PerPage: rowsPerPage,
			Page:    page,
		}
		prs, _, err := client.PullRequests.List(ctx, r.Owner, r.Name, &opts)
		if err != nil {
			return nil, errors.Wrapf(err, "Failed to list pull requests in %s/%s - at page %d", r.Owner, r.Name, page)
		}

		pulls = append(pulls, prs...)
		if len(prs) < rowsPerPage {
			cont = false
			break
		}
	}

	return pulls, nil
}

// ListPRFiles returns a list of all files included in a given PR in the repo.
func (r *Repo) ListPRFiles(ctx context.Context, client *github.Client, pr int) (allFiles []*github.CommitFile, err error) {
	cont := true
	for page := 1; cont; page++ {
		files, _, err := client.PullRequests.ListFiles(ctx, r.Owner, r.Name, pr, &github.ListOptions{
			Page:    page,
			PerPage: rowsPerPage,
		})
		if err != nil {
			return nil, errors.Wrapf(err, "Failed to list changed files in Pull Request %s/%s#%d - at page %d", r.Owner, r.Name, pr, page)
		}
		allFiles = append(allFiles, files...)
		if len(files) < rowsPerPage {
			cont = false
			break
		}
	}

	return allFiles, nil
}
