package installer

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/engine"
	"github.com/ushineko/fynstall/extension"
	"github.com/ushineko/fynstall/manifest"
)

// signIn is a page that sets account, asking on the command line.
func signIn() extension.Page {
	return extension.Page{
		ID: "sign-in", At: extension.After(extension.PageWelcome), Sets: []string{"account"},
		CLI: func(_ context.Context, c *extension.Context, p extension.Prompter) error {
			p.Say("Sign in first.")
			v, err := p.Ask("Account", "guest")
			if err != nil {
				return err
			}
			return c.SetParam("account", v)
		},
	}
}

func pagedManifest(required bool) *manifest.Manifest {
	return &manifest.Manifest{
		App:        manifest.App{ID: "io.example.keyed", Name: "Keyed", Version: "0.1.0"},
		Parameters: []manifest.Parameter{{Name: "account", Label: "Account", Required: required}, {Name: "colour", Default: "red"}},
	}
}

/*
Spec 003, the CLI rule. A page whose parameters were all given is passed
over. Otherwise its CLI function asks. An install that may not ask stops,
and names the flag, when the page's parameter is required and has no value;
it goes on when the parameter is not required.
*/
func TestTheCommandLinePassesAPageByTheRule(t *testing.T) {
	ext := &extension.Extension{Pages: []extension.Page{signIn()}}
	run := func(m *manifest.Manifest, flags map[string]string, answers string, ask bool) (map[string]string, string, error) {
		params, given, err := resolveParams(m, flags, nil, nil, nil, nil, ext.Sets()...)
		require.NoError(t, err, "a parameter a page sets is not held to required before its page")
		var out bytes.Buffer
		var a *asker
		if ask {
			a = newAsker(Env{In: strings.NewReader(answers), Out: &out})
		}
		c := installContext(m, fixed("user"), fixed(""), params, nil)
		err = cliPages(context.Background(), ext, m, c, given, a)
		if err == nil {
			err = requiredAfterPages(m, ext, params)
		}
		return params, out.String(), err
	}

	params, out, err := run(pagedManifest(true), map[string]string{"account": "ada"}, "", true)
	require.NoError(t, err)
	require.Equal(t, "ada", params["account"])
	require.Empty(t, out, "a page whose parameter came from a flag is not shown")

	params, out, err = run(pagedManifest(true), nil, "grace\n", true)
	require.NoError(t, err)
	require.Equal(t, "grace", params["account"])
	require.Contains(t, out, "Sign in first.")
	require.Contains(t, out, "Account [guest]: ")

	params, _, err = run(pagedManifest(true), nil, "\n", true)
	require.NoError(t, err)
	require.Equal(t, "guest", params["account"], "an empty answer is the prompt's default")

	_, _, err = run(pagedManifest(true), nil, "", false)
	require.ErrorContains(t, err, "--account=…", "a silent install names the flag that answers the page")
	require.ErrorContains(t, err, "sign-in")

	params, _, err = run(pagedManifest(false), nil, "", false)
	require.NoError(t, err, "a silent install passes a page whose parameter is not required")
	require.Empty(t, params["account"])
	require.Equal(t, "red", params["colour"], "the other parameters keep their defaults")
}

// A parameter a page sets is not asked for by the installer's own prompt:
// its page asks.
func TestTheInstallersOwnPromptLeavesAPagesParameterAlone(t *testing.T) {
	var asked []string
	ask := func(p manifest.Parameter) (string, error) {
		asked = append(asked, p.Name)
		return "", nil
	}
	_, _, err := resolveParams(pagedManifest(true), nil, nil, nil, nil, ask, "account")
	require.NoError(t, err)
	require.Equal(t, []string{"colour"}, asked)
}

// Pages are passed in the order the wizard shows them, whatever order the
// extension lists them in; pages at one place keep their order.
func TestPagesArePassedInTheWizardsOrder(t *testing.T) {
	page := func(id string, at extension.Position) extension.Page { return extension.Page{ID: id, At: at} }
	ext := &extension.Extension{Pages: []extension.Page{
		page("last", extension.Position{}),
		page("b", extension.After(extension.PageWelcome)),
		page("a", extension.After(extension.PageWelcome)),
		page("before-location", extension.Before(extension.PageLocation)),
	}}
	var ids []string
	for _, p := range orderedPages(ext) {
		ids = append(ids, p.ID)
	}
	require.Equal(t, []string{"b", "a", "before-location", "last"}, ids)
}

// The plan a person approves says that the program's own code will run
// (spec 003 R7), and an uninstall says so before it removes anything.
func TestThePlanSaysWhichHooksRun(t *testing.T) {
	hook := func(context.Context, *extension.Context) error { return nil }
	require.Empty(t, installHookLines(nil, "Keyed"))
	require.Empty(t, installHookLines(&extension.Extension{}, "Keyed"))
	require.Equal(t, []string{"before the install, runs Keyed's own set-up code", "after the install, runs Keyed's own set-up code"},
		installHookLines(&extension.Extension{BeforeInstall: hook, AfterInstall: hook}, "Keyed"))
	require.Equal(t, []string{"Before removing anything, it runs Keyed's own clean-up code."},
		uninstallHookLines(&extension.Extension{BeforeUninstall: hook}, "Keyed"))
}

// A Before* hook's error says nothing was changed; an After* hook's says
// the program is installed, or removed, and that its own code failed.
func TestAHooksErrorSaysWhatStateItLeft(t *testing.T) {
	boom := func(context.Context, *extension.Context) error { return errors.New("the licence server said no") }
	ext := &extension.Extension{BeforeInstall: boom, AfterInstall: boom, BeforeUninstall: boom, AfterUninstall: boom}
	c := installContext(pagedManifest(false), fixed("user"), fixed("/opt/keyed"), map[string]string{}, nil)
	ctx := context.Background()

	err := beforeInstall(ctx, ext, c)
	require.ErrorIs(t, err, errNothingChanged)
	require.ErrorContains(t, err, "the licence server said no")
	require.ErrorContains(t, afterInstall(ctx, ext, c), "Keyed 0.1.0 is installed, but its own set-up code failed")
	require.ErrorContains(t, beforeUninstall(ctx, ext, c), "nothing was removed")
	require.ErrorContains(t, afterUninstall(ctx, ext, c), "Keyed 0.1.0 was removed, but its own clean-up code failed")

	require.NoError(t, beforeInstall(ctx, nil, c))
	require.NoError(t, afterUninstall(ctx, &extension.Extension{}, c))
}

// An uninstall hook sees the parameters the receipt kept, where the program
// is, and whether an upgrade is what removes it.
func TestAnUninstallHookSeesTheInstallItRemoves(t *testing.T) {
	r := &engine.Receipt{
		App: manifest.App{ID: "io.example.keyed", Name: "Keyed", Version: "0.1.0"}, Scope: "user", Root: "/home/ada/keyed",
		Parameters: map[string]string{"account": "ada"},
	}
	c := uninstallContext(r, engine.ReasonUpgrade, nil)
	require.Equal(t, "ada", c.Param("account"))
	require.Equal(t, "/home/ada/keyed", c.Root())
	require.Equal(t, "user", c.Scope())
	require.True(t, c.Upgrade())
	require.False(t, uninstallContext(r, engine.ReasonUninstall, nil).Upgrade())
}

// --check-extension, which the builder runs on the installer it has built:
// an extension that breaks a rule fails it, and the rule is said.
func TestTheInstallerChecksItsOwnExtension(t *testing.T) {
	m := pagedManifest(true)
	var out, errOut bytes.Buffer
	e := Env{Out: &out, Err: &errOut}
	bad := &extension.Extension{Pages: []extension.Page{{ID: "sign-in", Sets: []string{"account"}}}}
	require.Equal(t, exitFail, checkExtension(m, bad, e))
	require.Contains(t, errOut.String(), "every page has a command-line path")
	require.Equal(t, exitOK, checkExtension(m, nil, e))
}
