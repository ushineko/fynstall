/*
Command hello is the program fynstall installs in its own tests and desk
checks: a fynedesygn window with an icon and an About section that shows its
version, so an upgrade can be seen (spec 001, phase 6).
*/
package main

import (
	_ "embed"

	"fyne.io/fyne/v2"
	fynetheme "fyne.io/fyne/v2/theme"

	"github.com/ushineko/fynedesygn/shell"
	"github.com/ushineko/fynedesygn/widgets"
)

// version is set with -ldflags "-X main.version=...", so two builds of the
// same source can be told apart once installed.
var version = "0.1.0"

//go:embed hello.png
var iconPNG []byte

var icon = fyne.NewStaticResource("hello.png", iconPNG)

func main() {
	shell.Run(options())
}

func options() shell.Options {
	return shell.Options{
		AppID:   "io.ushineko.hello",
		Name:    "Hello",
		Version: version,
		Icon:    icon,
		Sections: []shell.Section{
			shell.NewSection("Hello", fynetheme.HomeIcon, func(*shell.Shell) fyne.CanvasObject {
				return widgets.Heading("Hello", "A small program for fynstall to install.")
			}),
			shell.AboutSection(shell.About{
				Icon:    icon,
				Name:    "Hello",
				Version: version,
				Blurb:   "The example program that fynstall's tests and desk checks install and remove.",
				Facts:   []shell.Fact{{Label: "Licence", Value: "MIT"}},
			}),
		},
	}
}
