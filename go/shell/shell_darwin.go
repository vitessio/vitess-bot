//go:build darwin

package shell

func init() {
	globalRegexpOpt = "-E"
}
