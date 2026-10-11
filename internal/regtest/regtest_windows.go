/*
Package regtest gives a test a registry key of its own, so an install it
runs never writes the Uninstall entry or the PATH of the person who runs
the tests (see platform.RegistryRoot). Off Windows it does nothing.
*/
package regtest

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// Env is the variable that moves every registry key under a root.
const Env = "FYNSTALL_TEST_REGISTRY_ROOT"

// Root makes an empty key under HKCU\Software\fynstall-test and returns its
// path below HKCU, the value for Env. The key and everything under it is
// removed when the test ends.
func Root(t *testing.T) string {
	t.Helper()
	name := strings.NewReplacer("/", "-", `\`, "-").Replace(t.Name())
	for i := 0; ; i++ {
		root := fmt.Sprintf(`Software\fynstall-test\%s-%d`, name, i)
		k, existed, err := registry.CreateKey(registry.CURRENT_USER, root, registry.QUERY_VALUE)
		if err != nil {
			t.Fatalf("make the test's registry root: %v", err)
		}
		_ = k.Close()
		if existed {
			continue // left by a test that was killed, or in use by another
		}
		t.Cleanup(func() {
			if err := removeTree(root); err != nil {
				t.Errorf("remove the test's registry root: %v", err)
			}
			// The parent goes too once no test has a key in it; Windows
			// refuses to delete a key that still has one.
			_ = registry.DeleteKey(registry.CURRENT_USER, `Software\fynstall-test`)
		})
		return root
	}
}

// Snapshot lists every key and value under root, which is a path below
// HKCU, as "key" or "key\\value" to a description, so two snapshots can be
// compared with one assertion.
func Snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	var walk func(path string)
	walk = func(path string) {
		k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.READ)
		if err != nil {
			t.Fatalf("read the registry: %v", err)
		}
		defer func() { _ = k.Close() }()
		rel := strings.TrimPrefix(strings.TrimPrefix(path, root), `\`)
		out[rel] = "key"
		names, err := k.ReadValueNames(-1)
		if err != nil {
			t.Fatalf("read the registry: %v", err)
		}
		for _, n := range names {
			buf := make([]byte, 1<<16)
			size, kind, err := k.GetValue(n, buf)
			if err != nil {
				t.Fatalf("read the registry: %v", err)
			}
			out[rel+`\\`+n] = fmt.Sprintf("kind %d: %x", kind, buf[:size])
		}
		subs, err := k.ReadSubKeyNames(-1)
		if err != nil {
			t.Fatalf("read the registry: %v", err)
		}
		sort.Strings(subs)
		for _, s := range subs {
			walk(path + `\` + s)
		}
	}
	walk(root)
	return out
}

// Set writes a string value under root, making its key: what was in the
// registry before the install a test runs. expand makes it REG_EXPAND_SZ.
func Set(t *testing.T, root, key, name, data string, expand bool) {
	t.Helper()
	k, _, err := registry.CreateKey(registry.CURRENT_USER, root+`\`+key, registry.SET_VALUE)
	if err == nil {
		defer func() { _ = k.Close() }()
		if expand {
			err = k.SetExpandStringValue(name, data)
		} else {
			err = k.SetStringValue(name, data)
		}
	}
	if err != nil {
		t.Fatalf("write the registry: %v", err)
	}
}

// Get reads a string or number value under root as text. A missing key or
// value gives "" and false.
func Get(t *testing.T, root, key, name string) (string, bool) {
	t.Helper()
	k, err := registry.OpenKey(registry.CURRENT_USER, root+`\`+key, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false
	}
	if err != nil {
		t.Fatalf("read the registry: %v", err)
	}
	defer func() { _ = k.Close() }()
	if s, _, err := k.GetStringValue(name); err == nil {
		return s, true
	}
	n, _, err := k.GetIntegerValue(name)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false
	}
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return fmt.Sprint(n), true
}

// removeTree removes a key the tests made, with the keys under it. It is a
// test's cleanup of its own key; the installer has nothing like it.
func removeTree(path string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.READ)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	subs, err := k.ReadSubKeyNames(-1)
	_ = k.Close()
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	for _, s := range subs {
		if err := removeTree(path + `\` + s); err != nil {
			return err
		}
	}
	if err := registry.DeleteKey(registry.CURRENT_USER, path); err != nil {
		return fmt.Errorf("delete %s: %w", path, err)
	}
	return nil
}
