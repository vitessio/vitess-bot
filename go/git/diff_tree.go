package git

import (
	"os"
	"path/filepath"
	"regexp"

	"github.com/google/go-github/v53/github"
)

/*
:100644 000000 5716ca5987cbf97d6bb54920bea6adde242d87e6 0000000000000000000000000000000000000000 D	bar/bar.txt
:000000 100644 0000000000000000000000000000000000000000 76018072e09c5d31c8c6e3113b8aa0fe625195ca A	baz.txt
:100644 100644 257cc5642cb1a054f08cc83f2d943e56fd3ebe99 b210800439ffe3f2db0d47d9aab1969b38a770a5 M	foo.txt
*/

var diffTreeEntryRegexp = regexp.MustCompile(`^:(?P<oldmode>\d{6}) (?P<newmode>\d{6}) (?P<oldsha>[a-f0-9]{40}) (?P<newsha>[a-f0-9]{40}) [A-Z]\W(?P<path>.*)$`)

// See https://docs.github.com/en/rest/git/trees?apiVersion=2022-11-28#create-a-tree
func ParseDiffTreeEntry(line string, basedir string) (*github.TreeEntry, *github.Blob, error) {
	match := diffTreeEntryRegexp.FindStringSubmatch(line)
	if match == nil {
		return nil, nil, nil
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

		return &entry, nil, nil
	}

	content, err := os.ReadFile(filepath.Join(basedir, path))
	if err != nil {
		return nil, nil, err
	}

	entry.Content = github.String(string(content))

	return &entry, &github.Blob{
		Content:  entry.Content,
		Encoding: github.String("utf-8"),
	}, nil
}
