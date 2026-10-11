package platform

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// RegistryRoot is the key under HKCU that every registry key of an
// install is moved under, HKLM keys too: "" (the real registry), or
// FYNSTALL_TEST_REGISTRY_ROOT for the tests, which must not write the
// Uninstall entry or the PATH of the person who runs them.
//
// It is honoured in an elevated process too. Developers run the tests from
// elevated consoles, and a variable that is ignored there sends the tests'
// writes to the real registry. A helper that was started through a UAC
// prompt, with more rights than the process that started it, drops the
// variable (package installer, helperChannel).
func RegistryRoot(env func(string) string) string {
	return strings.Trim(env("FYNSTALL_TEST_REGISTRY_ROOT"), `\`)
}

// hive splits "HKCU\Software\X" into the open root key and "Software\X".
func hive(key string) (registry.Key, string, string, error) {
	name, rest, _ := strings.Cut(key, `\`)
	switch name {
	case "HKCU":
		return registry.CURRENT_USER, name, rest, nil
	case "HKLM":
		return registry.LOCAL_MACHINE, name, rest, nil
	}
	return 0, "", "", fmt.Errorf("registry key %s: unknown root %q", key, name)
}

// RegCreateKey makes key and every key above it that is missing. It
// returns the keys it made, outermost first, so the caller can record each
// one and remove it again.
func RegCreateKey(key string) ([]string, error) {
	root, name, rest, err := hive(key)
	if err != nil {
		return nil, err
	}
	var made []string
	parts := strings.Split(rest, `\`)
	for i := range parts {
		sub := strings.Join(parts[:i+1], `\`)
		k, existed, err := registry.CreateKey(root, sub, registry.QUERY_VALUE)
		if err != nil {
			return made, fmt.Errorf("create registry key %s\\%s: %w", name, sub, err)
		}
		_ = k.Close()
		if !existed {
			made = append(made, name+`\`+sub)
		}
	}
	return made, nil
}

// RegGet reads the value name of key. A missing key or value is not an
// error: it reports false. A value of a kind other than a string or a
// 32-bit number is an error, because RegSet could not put it back.
func RegGet(key, name string) (RegValue, bool, error) {
	root, _, rest, err := hive(key)
	if err != nil {
		return RegValue{}, false, err
	}
	k, err := registry.OpenKey(root, rest, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return RegValue{}, false, nil
	}
	if err != nil {
		return RegValue{}, false, fmt.Errorf("open registry key %s: %w", key, err)
	}
	defer func() { _ = k.Close() }()
	_, kind, err := k.GetValue(name, nil)
	if errors.Is(err, registry.ErrNotExist) {
		return RegValue{}, false, nil
	}
	if err != nil {
		return RegValue{}, false, fmt.Errorf("read %s in %s: %w", name, key, err)
	}
	v := RegValue{Name: name}
	switch kind {
	case registry.SZ, registry.EXPAND_SZ:
		v.Kind = RegString
		if kind == registry.EXPAND_SZ {
			v.Kind = RegExpandString
		}
		if v.Data, _, err = k.GetStringValue(name); err != nil {
			return RegValue{}, false, fmt.Errorf("read %s in %s: %w", name, key, err)
		}
	case registry.DWORD:
		n, _, err := k.GetIntegerValue(name)
		if err != nil {
			return RegValue{}, false, fmt.Errorf("read %s in %s: %w", name, key, err)
		}
		v.Kind, v.Data = RegNumber, strconv.FormatUint(n, 10)
	default:
		return RegValue{}, false, fmt.Errorf("%s in %s is a registry value of a kind (%d) this installer cannot put back", name, key, kind)
	}
	return v, true, nil
}

// RegSet writes v into key, which must exist.
func RegSet(key string, v RegValue) error {
	root, _, rest, err := hive(key)
	if err != nil {
		return err
	}
	k, err := registry.OpenKey(root, rest, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open registry key %s: %w", key, err)
	}
	defer func() { _ = k.Close() }()
	switch v.Kind {
	case RegString:
		err = k.SetStringValue(v.Name, v.Data)
	case RegExpandString:
		err = k.SetExpandStringValue(v.Name, v.Data)
	case RegNumber:
		var n uint64
		if n, err = strconv.ParseUint(v.Data, 10, 32); err == nil {
			err = k.SetDWordValue(v.Name, uint32(n))
		}
	default:
		err = fmt.Errorf("unknown kind %q", v.Kind)
	}
	if err != nil {
		return fmt.Errorf("write %s in %s: %w", v.Name, key, err)
	}
	return nil
}

// RegDelete removes the value name of key. A value or key that is already
// gone is not an error.
func RegDelete(key, name string) error {
	root, _, rest, err := hive(key)
	if err != nil {
		return err
	}
	k, err := registry.OpenKey(root, rest, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open registry key %s: %w", key, err)
	}
	defer func() { _ = k.Close() }()
	if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("remove %s from %s: %w", name, key, err)
	}
	return nil
}

// RegDeleteKeyIfEmpty removes key when it has no values and no keys under
// it, and reports whether it is gone. A key that holds something is left:
// what it holds is not the install's.
func RegDeleteKeyIfEmpty(key string) (bool, error) {
	root, _, rest, err := hive(key)
	if err != nil {
		return false, err
	}
	k, err := registry.OpenKey(root, rest, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("open registry key %s: %w", key, err)
	}
	st, err := k.Stat()
	_ = k.Close()
	if err != nil {
		return false, fmt.Errorf("read registry key %s: %w", key, err)
	}
	if st.SubKeyCount > 0 || st.ValueCount > 0 {
		return false, nil
	}
	if err := registry.DeleteKey(root, rest); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return false, fmt.Errorf("remove registry key %s: %w", key, err)
	}
	return true, nil
}
