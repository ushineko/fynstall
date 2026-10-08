package platform

import (
	"errors"
	"path/filepath"
)

func vars(scope string, env func(string) string) (map[string]string, error) {
	if scope != "user" {
		return nil, unsupportedScope(scope)
	}
	home := env("HOME")
	if home == "" || !filepath.IsAbs(home) {
		return nil, errors.New("HOME is not set to an absolute path")
	}
	return map[string]string{
		"home":   home,
		"data":   absOr(env, "XDG_DATA_HOME", filepath.Join(home, ".local", "share")),
		"config": absOr(env, "XDG_CONFIG_HOME", filepath.Join(home, ".config")),
		"bin":    filepath.Join(home, ".local", "bin"),
	}, nil
}
