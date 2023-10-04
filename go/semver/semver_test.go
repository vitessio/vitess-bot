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

package semver

import "testing"

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tag    string
		expect string
	}{
		{
			tag:    "1.2.3",
			expect: "1.2.3",
		},
		{
			tag:    "v1.2.3",
			expect: "1.2.3",
		},
		{
			tag:    "v10.0.18",
			expect: "10.0.18",
		},
		{
			tag:    "v18.0.0-rc1",
			expect: "18.0.0-rc1",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.tag, func(t *testing.T) {
			t.Parallel()

			v, err := Parse(test.tag)
			if test.expect == "" {
				if err == nil {
					t.Fatalf("Parse(%s) should error; got %s", test.tag, v.String())
				}
			}

			if err != nil {
				t.Fatalf("Parse(%s) should not error; got %s", test.tag, err.Error())
			}

			if v.String() != test.expect {
				t.Fatalf("Parse(%s): want %s; got %s", test.tag, test.expect, v.String())
			}
		})
	}
}
