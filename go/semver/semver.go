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

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var versionRegexp = regexp.MustCompile(`(v)?(\d+)\.(\d+)\.(\d+)(-rc\d+)?`)

type Version struct {
	Major, Minor, Patch uint
	RCVersion           uint
}

func Parse(s string) (v Version, err error) {
	m := versionRegexp.FindStringSubmatch(s)
	if m == nil {
		return Version{}, fmt.Errorf("%s is not a valid semver (does not match %s)", s, versionRegexp.String())
	}

	major, err := strconv.ParseUint(m[2], 10, 64)
	if err != nil {
		return v, err
	}

	minor, err := strconv.ParseUint(m[3], 10, 64)
	if err != nil {
		return v, err
	}

	patch, err := strconv.ParseUint(m[4], 10, 64)
	if err != nil {
		return v, err
	}

	if len(m[5]) > 0 {
		// remove "-rc"
		rc, err := strconv.ParseUint(m[5][3:], 10, 64)
		if err != nil {
			return v, err
		}

		v.RCVersion = uint(rc)
	}

	v.Major = uint(major)
	v.Minor = uint(minor)
	v.Patch = uint(patch)

	return v, nil
}

func (v Version) String() string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "%d.%d.%d", v.Major, v.Minor, v.Patch)

	if v.RCVersion > 0 {
		fmt.Fprintf(&buf, "-rc%d", v.RCVersion)
	}

	return buf.String()
}
