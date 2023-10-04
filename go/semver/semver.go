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

func (v Version) NextMajor() Version {
	return Version{
		Major:     v.Major + 1,
		Minor:     0,
		Patch:     0,
		RCVersion: 0,
	}
}

func (v Version) PrevMajor() Version {
	if v.Major == 0 {
		panic(fmt.Sprintf("cannot decrement major version of %s", v.String()))
	}

	return Version{
		Major:     v.Major - 1,
		Minor:     0,
		Patch:     0,
		RCVersion: 0,
	}
}

func (v Version) NextMinor() Version {
	return Version{
		Major:     v.Major,
		Minor:     v.Minor + 1,
		Patch:     0,
		RCVersion: 0,
	}
}

func (v Version) PrevMinor() Version {
	if v.Minor == 0 {
		panic(fmt.Sprintf("cannot decrement minor version of %s", v.String()))
	}

	return Version{
		Major:     v.Major,
		Minor:     v.Minor - 1,
		Patch:     0,
		RCVersion: 0,
	}
}

func (v Version) NextPatch() Version {
	return Version{
		Major:     v.Major,
		Minor:     v.Minor,
		Patch:     v.Patch + 1,
		RCVersion: 0,
	}
}

func (v Version) PrevPatch() Version {
	if v.Patch == 0 {
		panic(fmt.Sprintf("cannot decrement patch version of %s", v.String()))
	}

	return Version{
		Major:     v.Major,
		Minor:     v.Minor,
		Patch:     v.Patch - 1,
		RCVersion: 0,
	}
}

func (v Version) NextRCVersion() Version {
	return Version{
		Major:     v.Major,
		Minor:     v.Minor,
		Patch:     v.Patch,
		RCVersion: v.RCVersion + 1,
	}
}

func (v Version) PrevRCVersion() Version {
	if v.RCVersion == 0 {
		panic(fmt.Sprintf("cannot decrement rc version of %s", v.String()))
	}

	return Version{
		Major:     v.Major,
		Minor:     v.Minor,
		Patch:     v.Patch,
		RCVersion: v.RCVersion - 1,
	}
}
