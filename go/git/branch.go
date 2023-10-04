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
	"strings"

	"github.com/google/go-github/v53/github"
)

// CreateBranch uses the github client to create a branch with the provided name
// and based on the provided ref in this repository.
func (r *Repo) CreateBranch(ctx context.Context, client *github.Client, base *github.Reference, name string) (ref *github.Reference, err error) {
	ref = &github.Reference{
		Ref: github.String("refs/heads/" + name),
		Object: &github.GitObject{
			SHA: base.GetObject().SHA,
		},
	}

	_, _, err = client.Git.CreateRef(ctx, r.Owner, r.Name, ref)
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return nil, err
	}

	return ref, nil
}
