// Package sessiontest holds helpers for tests that drive a session: the demo
// pak file system and a deterministic "walker" command function.
package sessiontest

import (
	"testing"

	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/testutil"
)

// DemoFS opens the demo pak (skipping the test when it is missing) and
// closes it when the test ends.
func DemoFS(t testing.TB) *pak.FS {
	t.Helper()
	pk, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	fs := &pak.FS{}
	fs.AddPak(pk)
	t.Cleanup(func() { _ = fs.Close() })
	return fs
}

// Walker returns a deterministic command function: it runs forward while
// slowly turning (one degree of yaw per command), jumps once a second and
// fires a short burst every two seconds, so a run produces movement,
// collisions, muzzle flashes and temp entities. The view angles are absolute
// (delta_angles are subtracted), so the path only depends on the commands.
// Each call returns a fresh walker.
func Walker() session.CmdFunc {
	n := 0
	return func(c *fakeclient.Client, msec int) shared.UserCmd {
		n++
		yaw := float32(n % 360)
		delta := c.Frame.PlayerState.PMove.DeltaAngles
		cmd := shared.UserCmd{ForwardMove: 300}
		cmd.Angles[q2const.YAW] = int16(shared.ANGLE2SHORT(yaw) - int32(delta[q2const.YAW]))
		cmd.Angles[q2const.PITCH] = int16(-int32(delta[q2const.PITCH]))
		if n%40 == 0 {
			cmd.UpMove = 200
		}
		if n%80 < 4 {
			cmd.Buttons = q2const.BUTTON_ATTACK
		}
		return cmd
	}
}
