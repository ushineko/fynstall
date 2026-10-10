package platform

import (
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// Integration is the Windows shell's: a launcher entry is a Start Menu
// shortcut, a link is an entry on PATH, and every install has an Uninstall
// registry entry (spec 001 R18, spec 002 L8 and L10).
const Integration = WindowsShell

func vars(scope string, env func(string) string) (map[string]string, error) {
	switch scope {
	case "user":
		home := env("USERPROFILE")
		if home == "" || !filepath.IsAbs(home) {
			return nil, errors.New("USERPROFILE is not set to an absolute path")
		}
		// {data} is the local, per-machine folder: an installed program does
		// not roam. {config} roams with the profile, as settings do. There
		// is no {bin}: Windows has no directory of links, and a link becomes
		// a PATH entry instead (spec 002 L10).
		data := absOr(env, "LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
		roaming := absOr(env, "APPDATA", filepath.Join(home, "AppData", "Roaming"))
		return map[string]string{
			"home":   home,
			"data":   data,
			"config": roaming,
			// Where Windows puts a program installed for one user (R18).
			"programs": filepath.Join(data, "Programs"),
			keyScope:   scope, keyIndex: filepath.Join(data, "fynstall", "installs"),
			keyStartMenu: filepath.Join(roaming, "Microsoft", "Windows", "Start Menu", "Programs"),
			keyRegRoot:   RegistryRoot(env),
		}, nil
	case "system":
		// The machine's folders, asked of Windows and not of the
		// environment: the helper that writes them is an administrator, and
		// must not be sent elsewhere by a variable. There is no {home} and
		// no {bin}. {data} and {config} are both the machine's data folder:
		// Windows has one.
		root, reg := SystemRoot(env), RegistryRoot(env)
		var programs, data, menu string
		if root != "" {
			if reg == "" {
				return nil, errors.New("FYNSTALL_TEST_SYSTEM_ROOT needs FYNSTALL_TEST_REGISTRY_ROOT too: a test of a system install must not write the machine's registry")
			}
			programs, data = filepath.Join(root, "Program Files"), filepath.Join(root, "ProgramData")
			menu = filepath.Join(data, "Microsoft", "Windows", "Start Menu", "Programs")
		} else {
			var err error
			for id, dst := range map[*windows.KNOWNFOLDERID]*string{
				windows.FOLDERID_ProgramFiles: &programs, windows.FOLDERID_ProgramData: &data, windows.FOLDERID_CommonPrograms: &menu,
			} {
				if *dst, err = windows.KnownFolderPath(id, windows.KF_FLAG_DEFAULT); err != nil {
					return nil, fmt.Errorf("find the machine's folders: %w", err)
				}
			}
		}
		return map[string]string{
			"data":     data,
			"config":   data,
			"programs": programs,
			keyScope:   scope, keyIndex: filepath.Join(data, "fynstall", "installs"), keyRoot: root,
			keyStartMenu: menu,
			keyRegRoot:   reg,
		}, nil
	}
	return nil, unsupportedScope(scope)
}

// SystemRoot is the directory every path of a system install is under: ""
// (the machine's own folders), or FYNSTALL_TEST_SYSTEM_ROOT for the tests,
// which run a system install without an administrator. Unlike on Linux it
// is honoured in an elevated process, as RegistryRoot is and for the same
// reason. The helper of a real system install drops both variables when it
// has more rights than the process that started it (package installer).
func SystemRoot(env func(string) string) string {
	if r := env("FYNSTALL_TEST_SYSTEM_ROOT"); r != "" && filepath.IsAbs(r) {
		return filepath.Clean(r)
	}
	return ""
}
