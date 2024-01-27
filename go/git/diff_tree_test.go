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
	"testing"

	"github.com/google/go-github/v53/github"
)

func TestParseDiffTreeEntry(t *testing.T) {
	// TODO: create fake filesystem
	// baz.txt contains: baz
	// foo.txt contains foo\nfoo2 after the edit
	tcases := []struct {
		name    string
		in      string
		want    *github.TreeEntry
		wantErr bool
	}{
		{
			name: "deleted file",
			in:   ":100644 000000 5716ca5987cbf97d6bb54920bea6adde242d87e6 0000000000000000000000000000000000000000 D	bar/bar.txt",
			want: &github.TreeEntry{
				SHA:  nil, // Indicates deletion
				Path: github.String("bar/bar.txt"),
				Mode: github.String("100644"),
				Type: github.String("blob"),
			},
		},
		{
			name: "created file",
			in:   ":000000 100644 0000000000000000000000000000000000000000 76018072e09c5d31c8c6e3113b8aa0fe625195ca A	baz.txt",
			want: &github.TreeEntry{
				Path:    github.String("baz.txt"),
				Mode:    github.String("100644"),
				Type:    github.String("blob"),
				Content: github.String("baz"),
			},
		},
		{
			name: "modified file",
			in:   ":100644 100644 257cc5642cb1a054f08cc83f2d943e56fd3ebe99 b210800439ffe3f2db0d47d9aab1969b38a770a5 M	foo.txt",
			want: &github.TreeEntry{
				Path:    github.String("foo.txt"),
				Mode:    github.String("100644"),
				Type:    github.String("blob"),
				Content: github.String("foo"),
			},
		},
	}

	for _, tc := range tcases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// entry, err := ParseDiffTreeEntry(tc.in, "TODO")
			if tc.wantErr {
				// TODO:
			}
		})
	}
}
