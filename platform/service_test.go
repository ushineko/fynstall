package platform

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// An account of its own is a system unit's dynamic user and its state
// directory. A user unit runs as its user already, and a unit without the
// key is as it was (spec 010 R3, R4).
func TestAnAccountOfItsOwnIsADynamicUserOfASystemUnit(t *testing.T) {
	base := Unit{AppID: "io.example.hello", Description: "Hello", Exec: "/opt/hello/bin/hello", Restart: "on-failure"}
	for _, c := range []struct {
		name        string
		system, own bool
		dynamic     bool
	}{
		{"a system unit of its own", true, true, true},
		{"a system unit", true, false, false},
		{"a user unit of its own", false, true, false},
		{"a user unit", false, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			u := base
			u.System, u.Own, u.StateDir = c.system, c.own, "hello"
			unit := string(RenderUnit(u))
			if c.dynamic {
				require.Contains(t, unit, "Restart=on-failure\nDynamicUser=yes\nStateDirectory=hello\n\n[Install]")
				return
			}
			require.NotContains(t, unit, "DynamicUser")
			require.NotContains(t, unit, "StateDirectory")
			require.Contains(t, unit, "Restart=on-failure\n\n[Install]")
		})
	}
}

func TestTheVirtualAccountIsNamedForTheService(t *testing.T) {
	require.Equal(t, `NT SERVICE\hotaru`, VirtualAccount("hotaru"))
}
