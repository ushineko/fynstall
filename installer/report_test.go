package installer

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/engine"
)

func TestTheProgressLineIsForTerminalsOnly(t *testing.T) {
	events := []engine.Event{
		{Kind: engine.Step, Text: engine.StepFiles},
		{Kind: engine.Progress, Text: "a", Counts: engine.Counts{Files: 1, FilesTotal: 2, Bytes: 10, BytesTotal: 2048}},
		{Kind: engine.Progress, Text: "b", Counts: engine.Counts{Files: 2, FilesTotal: 2, Bytes: 2048, BytesTotal: 2048}},
		{Kind: engine.Step, Text: engine.StepRecord},
	}
	run := func(terminal bool) string {
		var out bytes.Buffer
		r := reporter(Env{Out: &out, Err: &out, OutTerminal: terminal}, false)
		for _, ev := range events {
			r(ev)
		}
		return out.String()
	}
	plain := run(false)
	require.Equal(t, "==> Copying files\n==> Recording the install\n", plain, "a log or a pipe gets no progress line")
	tty := run(true)
	require.Contains(t, tty, "\r\033[K    1 of 2 files · 10 B of 2.0 KB")
	require.Contains(t, tty, "2 of 2 files · 2.0 KB of 2.0 KB", "the last count is always drawn")
	require.True(t, strings.HasSuffix(tty, "\r\033[K==> Recording the install\n"), "the line is cleared before the next step: %q", tty)
}

func TestThousandsAndHumanSize(t *testing.T) {
	require.Equal(t, "5,603", thousands(5603))
	require.Equal(t, "1,234,567", thousands(1234567))
	require.Equal(t, "999", thousands(999))
	require.Equal(t, "512 B", humanSize(512))
	require.Equal(t, "3.4 MB", humanSize(3_565_158))
	require.Equal(t, "118 MB", humanSize(118<<20))
}
