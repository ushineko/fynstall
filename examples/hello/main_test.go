package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynedesygn/fynetest"
	"github.com/ushineko/fynedesygn/shell"
)

func TestEverySectionBuildsAndAboutShowsTheVersion(t *testing.T) {
	s := shell.Headless(fynetest.App(t), options())
	var about string
	for _, sec := range s.Sections() {
		o := sec.Build(s)
		require.NotNil(t, o, sec.Title())
		if sec.Title() == "About" {
			about = fynetest.Text(o)
		}
	}
	require.Contains(t, about, version, "an installed upgrade is seen through this version")
}
