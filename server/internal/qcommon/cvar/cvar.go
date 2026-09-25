// Package cvar ports qcommon/cvar.c: dynamic console variables. All C globals
// (cvar_vars, userinfo_modified) live on a per-instance Registry.
package cvar

import (
	"fmt"
	"math"
	"strings"

	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// Cvar mirrors cvar_t. Nothing outside the Registry methods should modify
// these fields.
// C: game/q_shared.h:325 cvar_t
type Cvar struct {
	Name          string
	String        string
	LatchedString *string // for CVAR_LATCH vars; nil is C NULL
	Flags         int
	Modified      bool    // set each time the cvar is changed
	Value         float32 // (float)atof(String)
	next          *Cvar
}

// Args is the view of the tokenized command line that Cvar_Command and the
// console commands need (implemented by cmd.Cmd). It avoids an import cycle.
type Args interface {
	Argc() int
	Argv(i int) string
}

// CommandAdder registers console commands (implemented by cmd.Cmd).
type CommandAdder interface {
	Args
	AddCommand(name string, fn func())
}

// Registry holds every cvar of one engine instance.
type Registry struct {
	vars *Cvar // newest first, like the C linked list

	// UserinfoModified is C userinfo_modified.
	UserinfoModified bool

	// Printf receives Com_Printf output; nil discards it.
	Printf func(format string, args ...any)
	// ServerState is C Com_ServerState(); nil means 0 (no server running).
	ServerState func() int
	// SetGamedir and ExecAutoexec are FS_SetGamedir / FS_ExecAutoexec, called
	// when the "game" cvar changes. Either may be nil.
	SetGamedir   func(dir string)
	ExecAutoexec func()
}

// New returns an empty registry.
func New() *Registry { return &Registry{} }

func (r *Registry) printf(format string, args ...any) {
	if r.Printf != nil {
		r.Printf(format, args...)
	}
}

func (r *Registry) serverState() int {
	if r.ServerState == nil {
		return 0
	}
	return r.ServerState()
}

func (r *Registry) gameChanged(v *Cvar) {
	if v.Name == "game" {
		if r.SetGamedir != nil {
			r.SetGamedir(v.String)
		}
		if r.ExecAutoexec != nil {
			r.ExecAutoexec()
		}
	}
}

// cstr truncates at the first NUL, like C string functions see it.
func cstr(s string) string {
	if i := strings.IndexByte(s, 0); i >= 0 {
		return s[:i]
	}
	return s
}

func atof(s string) float32 { return float32(shared.Atof(s)) }

// Each iterates over all cvars in C list order (newest first).
func (r *Registry) Each(fn func(v *Cvar)) {
	for v := r.vars; v != nil; v = v.next {
		fn(v)
	}
}

// Cvar_InfoValidate rejects characters that break info strings.
// C: qcommon/cvar.c:31 Cvar_InfoValidate
func infoValidate(s string) bool {
	if strings.Contains(s, "\\") {
		return false
	}
	if strings.Contains(s, "\"") {
		return false
	}
	if strings.Contains(s, ";") {
		return false
	}
	return true
}

// FindVar returns the named cvar or nil.
// C: qcommon/cvar.c:47 Cvar_FindVar
func (r *Registry) FindVar(name string) *Cvar {
	name = cstr(name)
	for v := r.vars; v != nil; v = v.next {
		if name == v.Name {
			return v
		}
	}
	return nil
}

// VariableValue returns (float)atof of the cvar string, or 0.
// C: qcommon/cvar.c:63 Cvar_VariableValue
func (r *Registry) VariableValue(name string) float32 {
	v := r.FindVar(name)
	if v == nil {
		return 0
	}
	return atof(v.String)
}

// VariableString returns the cvar string, or "".
// C: qcommon/cvar.c:79 Cvar_VariableString
func (r *Registry) VariableString(name string) string {
	v := r.FindVar(name)
	if v == nil {
		return ""
	}
	return v.String
}

// CompleteVariable returns the first exact, then prefix, match ("" = NULL).
// C: qcommon/cvar.c:95 Cvar_CompleteVariable
func (r *Registry) CompleteVariable(partial string) string {
	partial = cstr(partial)
	if len(partial) == 0 {
		return ""
	}
	// check exact match
	for v := r.vars; v != nil; v = v.next {
		if partial == v.Name {
			return v.Name
		}
	}
	// check partial match
	for v := r.vars; v != nil; v = v.next {
		if strings.HasPrefix(v.Name, partial) {
			return v.Name
		}
	}
	return ""
}

// Get returns the cvar, creating it with value if it does not exist. If it
// exists the value is not changed and flags are or'ed in. Returns nil where C
// returns NULL (invalid info name/value).
// C: qcommon/cvar.c:127 Cvar_Get
func (r *Registry) Get(name, value string, flags int) *Cvar {
	return r.get(cstr(name), &value, flags)
}

// GetNull is Cvar_Get(name, NULL, flags): it never creates the cvar.
// C: qcommon/cvar.c:127 Cvar_Get
func (r *Registry) GetNull(name string, flags int) *Cvar {
	return r.get(cstr(name), nil, flags)
}

func (r *Registry) get(name string, value *string, flags int) *Cvar {
	if flags&(q2const.CVAR_USERINFO|q2const.CVAR_SERVERINFO) != 0 {
		if !infoValidate(name) {
			r.printf("invalid info cvar name\n")
			return nil
		}
	}

	if v := r.FindVar(name); v != nil {
		v.Flags |= flags
		return v
	}

	if value == nil {
		return nil
	}
	val := cstr(*value)

	if flags&(q2const.CVAR_USERINFO|q2const.CVAR_SERVERINFO) != 0 {
		if !infoValidate(val) {
			r.printf("invalid info cvar value\n")
			return nil
		}
	}

	v := &Cvar{
		Name:     name,
		String:   val,
		Modified: true,
	}
	v.Value = atof(v.String)

	// link the variable in
	v.next = r.vars
	r.vars = v

	v.Flags = flags
	return v
}

// Set2 sets a cvar, honoring CVAR_NOSET and CVAR_LATCH unless force.
// C: qcommon/cvar.c:179 Cvar_Set2
func (r *Registry) Set2(name, value string, force bool) *Cvar {
	name, value = cstr(name), cstr(value)
	v := r.FindVar(name)
	if v == nil { // create it
		return r.Get(name, value, 0)
	}

	if v.Flags&(q2const.CVAR_USERINFO|q2const.CVAR_SERVERINFO) != 0 {
		if !infoValidate(value) {
			r.printf("invalid info cvar value\n")
			return v
		}
	}

	if !force {
		if v.Flags&q2const.CVAR_NOSET != 0 {
			r.printf("%s is write protected.\n", name)
			return v
		}

		if v.Flags&q2const.CVAR_LATCH != 0 {
			if v.LatchedString != nil {
				if value == *v.LatchedString {
					return v
				}
				// C frees latched_string here but leaves the pointer
				// dangling; we drop it (memory-safety), see below.
				v.LatchedString = nil
			} else {
				if value == v.String {
					return v
				}
			}

			if r.serverState() != 0 {
				r.printf("%s will be changed for next game.\n", name)
				s := value
				v.LatchedString = &s
			} else {
				v.String = value
				v.Value = atof(v.String)
				r.gameChanged(v)
			}
			return v
		}
	} else {
		v.LatchedString = nil
	}

	if value == v.String {
		return v // not changed
	}

	v.Modified = true

	if v.Flags&q2const.CVAR_USERINFO != 0 {
		r.UserinfoModified = true // transmit at next oportunity
	}

	v.String = value
	v.Value = atof(v.String)
	return v
}

// ForceSet sets a cvar ignoring NOSET/LATCH.
// C: qcommon/cvar.c:268 Cvar_ForceSet
func (r *Registry) ForceSet(name, value string) *Cvar {
	return r.Set2(name, value, true)
}

// Set sets a cvar like the console does.
// C: qcommon/cvar.c:278 Cvar_Set
func (r *Registry) Set(name, value string) *Cvar {
	return r.Set2(name, value, false)
}

// FullSet sets value and replaces flags.
// C: qcommon/cvar.c:288 Cvar_FullSet
func (r *Registry) FullSet(name, value string, flags int) *Cvar {
	name, value = cstr(name), cstr(value)
	v := r.FindVar(name)
	if v == nil { // create it
		return r.Get(name, value, flags)
	}

	v.Modified = true

	if v.Flags&q2const.CVAR_USERINFO != 0 {
		r.UserinfoModified = true // transmit at next oportunity
	}

	v.String = value
	v.Value = atof(v.String)
	v.Flags = flags
	return v
}

// FormatValue is the string Cvar_SetValue builds: "%i" when the float is
// integral, otherwise "%f", truncated to the 31 chars Com_sprintf keeps.
func FormatValue(value float32) string {
	var s string
	iv := int32(value) // x86 cvttss2si: out of range / NaN give INT_MIN
	if value == float32(iv) {
		s = fmt.Sprintf("%d", iv)
	} else {
		s = cFormatF(float64(value))
	}
	if len(s) > 31 {
		s = s[:31]
	}
	return s
}

// cFormatF is glibc printf("%f") of a double.
func cFormatF(d float64) string {
	switch {
	case math.IsNaN(d):
		if math.Signbit(d) {
			return "-nan"
		}
		return "nan"
	case math.IsInf(d, 1):
		return "inf"
	case math.IsInf(d, -1):
		return "-inf"
	}
	return fmt.Sprintf("%f", d)
}

// SetValue sets a cvar from a float.
// C: qcommon/cvar.c:317 Cvar_SetValue
func (r *Registry) SetValue(name string, value float32) {
	r.Set(name, FormatValue(value))
}

// GetLatchedVars applies every latched value.
// C: qcommon/cvar.c:336 Cvar_GetLatchedVars
func (r *Registry) GetLatchedVars() {
	for v := r.vars; v != nil; v = v.next {
		if v.LatchedString == nil {
			continue
		}
		v.String = *v.LatchedString
		v.LatchedString = nil
		v.Value = atof(v.String)
		r.gameChanged(v)
	}
}

// Command handles variable inspection and changing from the console.
// C: qcommon/cvar.c:363 Cvar_Command
func (r *Registry) Command(a Args) bool {
	// check variables
	v := r.FindVar(a.Argv(0))
	if v == nil {
		return false
	}

	// perform a variable print or set
	if a.Argc() == 1 {
		r.printf("\"%s\" is \"%s\"\n", v.Name, v.String)
		return true
	}

	r.Set(v.Name, a.Argv(1))
	return true
}

// Set_f allows setting and defining of arbitrary cvars from console.
// C: qcommon/cvar.c:391 Cvar_Set_f
func (r *Registry) Set_f(a Args) {
	c := a.Argc()
	if c != 3 && c != 4 {
		r.printf("usage: set <variable> <value> [u / s]\n")
		return
	}

	if c == 4 {
		var flags int
		switch a.Argv(3) {
		case "u":
			flags = q2const.CVAR_USERINFO
		case "s":
			flags = q2const.CVAR_SERVERINFO
		default:
			r.printf("flags can only be 'u' or 's'\n")
			return
		}
		r.FullSet(a.Argv(1), a.Argv(2), flags)
	} else {
		r.Set(a.Argv(1), a.Argv(2))
	}
}

// WriteVariables returns the lines "set variable value" for all archive
// cvars (C appends them to a file).
// C: qcommon/cvar.c:429 Cvar_WriteVariables
func (r *Registry) WriteVariables() string {
	var b strings.Builder
	for v := r.vars; v != nil; v = v.next {
		if v.Flags&q2const.CVAR_ARCHIVE != 0 {
			line := fmt.Sprintf("set %s \"%s\"\n", v.Name, v.String)
			if len(line) > 1023 { // Com_sprintf into buffer[1024]
				line = line[:1023]
			}
			b.WriteString(line)
		}
	}
	return b.String()
}

// List_f prints the cvar list (also returned as text).
// C: qcommon/cvar.c:453 Cvar_List_f
func (r *Registry) List_f() string {
	var b strings.Builder
	i := 0
	for v := r.vars; v != nil; v, i = v.next, i+1 {
		if v.Flags&q2const.CVAR_ARCHIVE != 0 {
			b.WriteString("*")
		} else {
			b.WriteString(" ")
		}
		if v.Flags&q2const.CVAR_USERINFO != 0 {
			b.WriteString("U")
		} else {
			b.WriteString(" ")
		}
		if v.Flags&q2const.CVAR_SERVERINFO != 0 {
			b.WriteString("S")
		} else {
			b.WriteString(" ")
		}
		if v.Flags&q2const.CVAR_NOSET != 0 {
			b.WriteString("-")
		} else if v.Flags&q2const.CVAR_LATCH != 0 {
			b.WriteString("L")
		} else {
			b.WriteString(" ")
		}
		fmt.Fprintf(&b, " %s \"%s\"\n", v.Name, v.String)
	}
	fmt.Fprintf(&b, "%d cvars\n", i)
	out := b.String()
	r.printf("%s", out)
	return out
}

// BitInfo returns an info string of all cvars with any of bit set.
// C: qcommon/cvar.c:488 Cvar_BitInfo
func (r *Registry) BitInfo(bit int) string {
	info := ""
	for v := r.vars; v != nil; v = v.next {
		if v.Flags&bit != 0 {
			var warn string
			info, warn = shared.Info_SetValueForKey(info, v.Name, v.String)
			if warn != "" {
				r.printf("%s", warn)
			}
		}
	}
	return info
}

// Userinfo returns an info string containing all the CVAR_USERINFO cvars.
// C: qcommon/cvar.c:504 Cvar_Userinfo
func (r *Registry) Userinfo() string { return r.BitInfo(q2const.CVAR_USERINFO) }

// Serverinfo returns an info string containing all the CVAR_SERVERINFO cvars.
// C: qcommon/cvar.c:510 Cvar_Serverinfo
func (r *Registry) Serverinfo() string { return r.BitInfo(q2const.CVAR_SERVERINFO) }

// Init registers the "set" and "cvarlist" commands.
// C: qcommon/cvar.c:522 Cvar_Init
func (r *Registry) Init(c CommandAdder) {
	c.AddCommand("set", func() { r.Set_f(c) })
	c.AddCommand("cvarlist", func() { r.List_f() })
}
