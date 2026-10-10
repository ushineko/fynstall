package platform

import (
	"errors"
	"os"
	"path/filepath"
)

// Integration is XDG: launcher entries and icons go under {data} and links
// in {bin} (spec 001 R15).
const Integration = XDG

func vars(scope string, env func(string) string) (map[string]string, error) {
	switch scope {
	case "user":
		home := env("HOME")
		if home == "" || !filepath.IsAbs(home) {
			return nil, errors.New("HOME is not set to an absolute path")
		}
		data := absOr(env, "XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
		return map[string]string{
			"home":   home,
			"data":   data,
			"config": absOr(env, "XDG_CONFIG_HOME", filepath.Join(home, ".config")),
			"bin":    filepath.Join(home, ".local", "bin"),
			keyScope: scope, keyIndex: filepath.Join(data, "fynstall", "installs"),
		}, nil
	case "system":
		// /usr/share and /usr/bin belong to the package manager; locally
		// installed software goes under /usr/local, and its state under
		// /var/lib (spec 001 R15). There is no {home}: a system install is
		// for every user.
		root := SystemRoot(env)
		return map[string]string{
			"data":   filepath.Join(root, "/usr/local/share"),
			"config": filepath.Join(root, "/etc"),
			"bin":    filepath.Join(root, "/usr/local/bin"),
			keyScope: scope, keyIndex: filepath.Join(root, "/var/lib/fynstall/installs"), keyRoot: root,
		}, nil
	}
	return nil, unsupportedScope(scope)
}

// SystemRoot is the directory every system path is under: "" (the real
// root), or FYNSTALL_TEST_SYSTEM_ROOT for the tests, which run a system
// install without root. The variable is ignored when running as root, so
// it can never move a real system install.
func SystemRoot(env func(string) string) string {
	if r := env("FYNSTALL_TEST_SYSTEM_ROOT"); r != "" && filepath.IsAbs(r) && os.Geteuid() != 0 {
		return r
	}
	return ""
}
