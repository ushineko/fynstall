package platform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestUserVarsComeFromTheProfile(t *testing.T) {
	env := map[string]string{"USERPROFILE": `C:\Users\ada`, "LOCALAPPDATA": `D:\local`, "APPDATA": `relative`}
	v, err := Vars("user", func(k string) string { return env[k] })
	require.NoError(t, err)
	require.Equal(t, `C:\Users\ada`, v["home"])
	require.Equal(t, `D:\local`, v["data"])
	require.Equal(t, `D:\local\Programs`, v["programs"])
	require.Equal(t, `C:\Users\ada\AppData\Roaming`, v["config"], "a relative value is ignored")
	require.Equal(t, `D:\local\fynstall\installs`, IndexDir(v))
	require.False(t, System(v))
	_, has := v["bin"]
	require.False(t, has, "Windows has no directory of links")

	_, err = Vars("user", func(string) string { return "" })
	require.ErrorContains(t, err, "USERPROFILE")
}

// A system install goes in the machine's folders, which Windows names, and
// has no {home} and no {bin}.
func TestSystemVarsAreTheMachinesFolders(t *testing.T) {
	v, err := Vars("system", func(string) string { return "" })
	require.NoError(t, err)
	require.True(t, System(v))
	for key, id := range map[string]*windows.KNOWNFOLDERID{"programs": windows.FOLDERID_ProgramFiles, "data": windows.FOLDERID_ProgramData, "config": windows.FOLDERID_ProgramData} {
		want, err := windows.KnownFolderPath(id, windows.KF_FLAG_DEFAULT)
		require.NoError(t, err)
		require.Equal(t, want, v[key], key)
	}
	require.Equal(t, filepath.Join(v["data"], "fynstall", "installs"), IndexDir(v))
	require.NotContains(t, v, "home")
	require.NotContains(t, v, "bin")
	require.Equal(t, SystemUninstallKey, UninstallKeyOf(v))
	require.Equal(t, SystemEnvironmentKey, EnvironmentKeyOf(v))

	_, err = Vars("machine", func(string) string { return "" })
	require.ErrorContains(t, err, `scope "machine" is not supported`)
}

// The tests' system root moves the files, and is refused without a
// registry root: a test of a system install would write HKLM.
func TestTheTestSystemRootNeedsARegistryRootAndMovesEverything(t *testing.T) {
	env := map[string]string{"FYNSTALL_TEST_SYSTEM_ROOT": `C:\tmp\sys`}
	_, err := Vars("system", func(k string) string { return env[k] })
	require.ErrorContains(t, err, "FYNSTALL_TEST_REGISTRY_ROOT")

	env["FYNSTALL_TEST_REGISTRY_ROOT"] = `Software\x`
	v, err := Vars("system", func(k string) string { return env[k] })
	require.NoError(t, err)
	require.Equal(t, `C:\tmp\sys\Program Files`, v["programs"])
	require.Equal(t, `C:\tmp\sys\ProgramData`, v["data"])
	require.Equal(t, `C:\tmp\sys\ProgramData\Microsoft\Windows\Start Menu\Programs`, StartMenu(v))
	require.Equal(t, `HKCU\Software\x\`+SystemUninstallKey, UninstallKeyOf(v))
	require.Equal(t, `C:\tmp\sys\Program Files\x`, Rooted(v, v["programs"]+`\x`), "under the root already")
	require.Equal(t, `C:\tmp\sys\Tools\x`, Rooted(v, `D:\Tools\x`), "a chosen directory goes under the root without its drive")
}

// The record of a system install is believed only when an administrator
// wrote it: any user can make a file in the machine's data folder.
func TestTheRecordOfASystemInstallIsTrustedOnlyFromAnAdministrator(t *testing.T) {
	system, err := Vars("system", func(string) string { return "" })
	require.NoError(t, err)
	user, err := Vars("user", func(k string) string { return map[string]string{"USERPROFILE": `C:\Users\ada`}[k] })
	require.NoError(t, err)
	p := filepath.Join(t.TempDir(), "io.example.x.json")
	require.NoError(t, os.WriteFile(p, []byte("{}"), 0o600))
	require.NoError(t, IndexTrusted(user, p), "a per-user record is the person's own")

	if !windows.GetCurrentProcessToken().IsElevated() {
		require.ErrorContains(t, IndexTrusted(system, p), "not written by an administrator")
		require.Error(t, ProtectIndex(system, p), "only an administrator can give a file to the administrators")
		return
	}
	// An elevated process: make the file somebody's own first.
	me, err := windows.GetCurrentProcessToken().GetTokenUser()
	require.NoError(t, err)
	require.NoError(t, windows.SetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION, me.User.Sid, nil, nil, nil))
	require.ErrorContains(t, IndexTrusted(system, p), "not written by an administrator")
	require.NoError(t, ProtectIndex(system, p))
	require.NoError(t, IndexTrusted(system, p))
}
