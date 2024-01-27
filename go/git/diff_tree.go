/*
Copyright 2024 The Vitess Authors.

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
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/google/go-github/v53/github"
)

/*
Example output of `git diff-tree -r HEAD~1 HEAD` in a sample repo:

:100644 000000 5716ca5987cbf97d6bb54920bea6adde242d87e6 0000000000000000000000000000000000000000 D	bar/bar.txt
:000000 100644 0000000000000000000000000000000000000000 76018072e09c5d31c8c6e3113b8aa0fe625195ca A	baz.txt
:100644 100644 257cc5642cb1a054f08cc83f2d943e56fd3ebe99 b210800439ffe3f2db0d47d9aab1969b38a770a5 M	foo.txt
*/
var diffTreeEntryRegexp = regexp.MustCompile(`^:(?P<oldmode>\d{6}) (?P<newmode>\d{6}) (?P<oldsha>[a-f0-9]{40}) (?P<newsha>[a-f0-9]{40}) [A-Z]\W(?P<path>.*)$`)

// ParseDiffTreeEntry parses a single line from `git diff-tree A B` into a
// TreeEntry object suitable to pass to github's CreateTree method.
//
// See https://docs.github.com/en/rest/git/trees?apiVersion=2022-11-28#create-a-tree.
func ParseDiffTreeEntry(line string, basedir string) (*github.TreeEntry, error) {
	match := diffTreeEntryRegexp.FindStringSubmatch(line)
	if match == nil {
		return nil, fmt.Errorf("invalid diff-tree line format %s", line)
	}

	oldMode := match[1]
	newMode := match[2]
	// oldSHA := match[3]
	// newSHA := match[4]
	path := match[5]

	entry := github.TreeEntry{
		Path: &path,
		Mode: &newMode,
		Type: github.String("blob"),
	}

	if newMode == "000000" {
		// File deleted.
		entry.Mode = &oldMode // GitHub API suggests sending 000000 will result in an error, and we're deleting the file anyway.
		entry.SHA = nil

		return &entry, nil
	}

	content, err := os.ReadFile(filepath.Join(basedir, path))
	if err != nil {
		return nil, err
	}

	entry.Content = github.String(string(content))

	return &entry, nil
}
