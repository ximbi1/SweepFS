package config

import "flag"

func ParseFlags(base Config) Config {
	path := flag.String("path", base.Path, "Initial path to scan")
	showHidden := flag.Bool("show-hidden", base.ShowHidden, "Show hidden files")
	safeMode := flag.Bool("safe-mode", base.SafeMode, "Enable safe mode protections")
	flag.Parse()

	base.Path = *path
	base.ShowHidden = *showHidden
	base.SafeMode = *safeMode
	return base
}
