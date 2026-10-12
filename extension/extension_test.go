package extension_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/extension"
)

func cli(context.Context, *extension.Context, extension.Prompter) error { return nil }

// Spec 003, the rules. An extension that keeps them passes; each rule
// broken is named, with the page.
func TestCheckHoldsAnExtensionToTheRules(t *testing.T) {
	declared := []string{"account", "token"}
	good := &extension.Extension{Pages: []extension.Page{
		{ID: "sign-in", At: extension.After(extension.PageWelcome), Sets: []string{"account"}, CLI: cli},
		{ID: "key", Sets: []string{"token"}, CLI: cli},
	}}
	require.NoError(t, good.Check(declared))
	require.NoError(t, (*extension.Extension)(nil).Check(declared), "no extension keeps every rule")

	for name, c := range map[string]struct {
		page extension.Page
		want string
	}{
		"no command-line path": {extension.Page{ID: "a", Sets: []string{"account"}}, "every page has a command-line path"},
		"an undeclared parameter": {extension.Page{ID: "a", Sets: []string{"colour"}, CLI: cli},
			`"colour", which is not a parameter of fynstall.yaml`},
		"an ID that is not one": {extension.Page{ID: "Sign In", CLI: cli}, "lowercase letters, digits and dashes"},
		"a page of the wizard that does not exist": {extension.Page{ID: "a", At: extension.After("payment"), CLI: cli},
			`"payment" is not a page of the wizard`},
		"after the summary":  {extension.Page{ID: "a", At: extension.After(extension.PageSummary), CLI: cli}, "before the summary"},
		"after the install":  {extension.Page{ID: "a", At: extension.Before(extension.PageFinish), CLI: cli}, "before the summary"},
		"before the welcome": {extension.Page{ID: "a", At: extension.Before(extension.PageWelcome), CLI: cli}, "before the welcome"},
	} {
		err := (&extension.Extension{Pages: []extension.Page{c.page}}).Check(declared)
		require.ErrorContains(t, err, c.want, name)
	}

	twice := &extension.Extension{Pages: []extension.Page{
		{ID: "a", Sets: []string{"account"}, CLI: cli},
		{ID: "a", Sets: []string{"account"}, CLI: cli},
	}}
	err := twice.Check(declared)
	require.ErrorContains(t, err, "two pages have this ID")
	require.ErrorContains(t, err, `sets "account", which page "a" sets too`)
}

// A place before a page ranks below it and a place after it above, in the
// wizard's order; the zero place is before the summary.
func TestAPlaceRanksBesideItsPage(t *testing.T) {
	welcome, licence, summary := extension.Rank(extension.PageWelcome), extension.Rank(extension.PageLicence), extension.Rank(extension.PageSummary)
	require.Less(t, welcome, licence)
	after := extension.After(extension.PageWelcome).Rank()
	require.Greater(t, after, welcome)
	require.Less(t, after, licence, "after the welcome is before the licence, shown or not")
	require.Less(t, extension.Before(extension.PageLicence).Rank(), licence)
	require.Greater(t, extension.Before(extension.PageLicence).Rank(), after, "before the licence comes after what is after the welcome")
	zero := extension.Position{}.Rank()
	require.Less(t, zero, summary)
	require.Greater(t, zero, extension.Rank(extension.PageLocation))
}

// A Context reads and sets declared parameters, and refuses any other: an
// install uses only what its plan shows.
func TestAContextSetsOnlyDeclaredParameters(t *testing.T) {
	params := map[string]string{"account": "ada"}
	var logged []string
	c := extension.NewContext(extension.Host{
		App:   extension.App{ID: "io.example.keyed", Name: "Keyed", Version: "0.1.0"},
		Scope: func() string { return "user" }, Params: params, Declared: []string{"account"},
		Log: func(_ extension.Level, text string) { logged = append(logged, text) },
	})
	require.Equal(t, "ada", c.Param("account"))
	require.NoError(t, c.SetParam("account", "grace"))
	require.Equal(t, "grace", params["account"], "the front end reads what the page set")
	require.ErrorContains(t, c.SetParam("colour", "red"), "not a parameter of fynstall.yaml")
	require.Empty(t, c.Root(), "no directory is chosen yet")
	require.Equal(t, "user", c.Scope())
	require.False(t, c.Upgrade())
	c.Logf(extension.Info, "signed in as %s", c.Param("account"))
	require.Equal(t, []string{"signed in as grace"}, logged)
}
