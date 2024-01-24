//go:build unix && !darwin

package shell

func init() {
	regexpTypeOpt = []string{
		// This is not a typo. It is "regexp" in goland and "regex" in POSIX land. ¯\_(ツ)_/¯
		"-regextype",
		"posix-extended",
	}
}
