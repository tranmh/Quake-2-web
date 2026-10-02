// Package perception turns what the bot's client received from the server
// into what a human player at the same screen and speakers could perceive.
//
// The pipeline has three stages:
//
//   - Reader (input.go) copies one FrameInput out of a fakeclient.Client after
//     every new valid server frame: the frame's packet entities (already PVS
//     filtered by the server), the player state, the area bits, the
//     configstrings and the events (prints, sounds, temp entities, muzzle
//     flashes, layouts, inventory) that arrived since the previous frame.
//     A FrameInput is a plain value: it can be recorded, replayed and, for
//     the fairness tests, rewritten.
//   - Classifier (classify.go), AnimCache (anim.go) and the sound table
//     (sound.go) name what an entity, frame or sound is, from the model,
//     frame and sound names a player would recognize. The priors attached
//     to a class (health, damage per second, range, item value) are player
//     knowledge, not game state.
//   - Perceiver (filter.go) is the ObservationFilter, the fairness core. Its
//     Percept holds only what is admissible: entities in the field of view
//     with a line of sight from the eye (Vision, vision.go), sounds, muzzle
//     flashes and explosions the client's mixer would make audible, visible
//     temp entities, the own player state and the HUD texts. An entity that
//     is in the packet but occluded or outside the view contributes nothing.
//
// The world model (package worldmodel) only ever sees Percepts.
//
// Fairness assumptions (also stated in docs/adr/0006-ai-agent.md):
// the field of view is the client's fov (player_state_t.fov, from the "fov"
// userinfo) at a 4:3 aspect unless configured; monster bodies do not occlude
// the line of sight; brush entities do (an occluding brush is by definition
// visible where the line of sight hits it); entity numbers identify an
// entity across frames, which stands for a player's visual re-identification;
// a sound tells the listener only what the client's mixer renders of it
// (S_SpatializeOrigin): a stereo balance and a distance attenuation, which
// a Hearing carries as a coarse Cue (Pan: five steps of left/right, with
// ahead and behind alike; Loudness: near, mid, far), never the emitter's
// position. Muzzle flashes and explosions heard but not seen are the same;
// seen ones carry what is seen. A loop sound is the blend the mixer makes
// of every entity of the frame with that sound (S_AddLoopSounds): one
// Hearing per sound, with the cue of the sum and no entity number. A sound
// or muzzle flash whose emitter is not in the packet arrives without a cue
// (the mixer would use a stale origin the Perceiver cannot know): it is
// passed on with Placed false, and the consumer must gate it with
// Percept.Audible at the position it believes the emitter is at; that the
// bot can tell this case (a player hears such a sound from the stale
// origin) is a stated assumption. A brush entity's sound (door, plat)
// admits the brush's pose. Without a collision map the Perceiver fails
// closed: nothing is seen, sounds are still heard.
//
// Nothing here imports the server or the game: the import guard test keeps
// the package on the client side of the wire.
package perception
