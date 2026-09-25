//go:build golden

package cmodel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/testutil"
)

// goldenMap loads maps/<name>.bsp through the baseq2 search path.
func goldenMap(t *testing.T, name string) *State {
	t.Helper()
	testutil.DemoPak(t)
	var fs pak.FS
	if err := fs.AddGameDirectory(filepath.Join(testutil.BaseDir(), "baseq2")); err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	raw, err := fs.ReadFile("maps/" + name + ".bsp")
	if err != nil {
		t.Skipf("map %s not available: %v", name, err)
	}
	m, err := LoadMapBytes("maps/"+name+".bsp", raw)
	if err != nil {
		t.Fatal(err)
	}
	return NewState(m)
}

type jBSP struct {
	Map            string `json:"map"`
	Checksum       uint32 `json:"checksum"`
	NumClusters    int    `json:"numclusters"`
	NumAreas       int    `json:"numareas"`
	NumLeafs       int    `json:"numleafs"`
	NumNodes       int    `json:"numnodes"`
	NumPlanes      int    `json:"numplanes"`
	NumBrushes     int    `json:"numbrushes"`
	NumBrushSides  int    `json:"numbrushsides"`
	NumLeafBrushes int    `json:"numleafbrushes"`
	NumTexInfo     int    `json:"numtexinfo"`
	NumAreaPortals int    `json:"numareaportals"`
	NumCModels     int    `json:"numcmodels"`
	EntSHA         string `json:"entitystring_sha256"`
	CModels        []struct {
		Mins     []float64 `json:"mins"`
		Maxs     []float64 `json:"maxs"`
		Origin   []float64 `json:"origin"`
		Headnode int32     `json:"headnode"`
	} `json:"cmodels"`
	PVS   []string `json:"pvs"`
	PHS   []string `json:"phs"`
	Leafs []struct {
		Contents int32 `json:"contents"`
		Cluster  int32 `json:"cluster"`
		Area     int32 `json:"area"`
	} `json:"leafs"`
}

func TestGoldenBSP(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(testutil.FixturesDir(), "core", "bsp", "*.json"))
	if len(files) == 0 {
		t.Skip("no core/bsp fixtures")
	}
	for _, path := range files {
		var g jBSP
		if err := testutil.ReadJSON(path, &g); err != nil {
			t.Fatal(err)
		}
		t.Run(g.Map, func(t *testing.T) {
			s := goldenMap(t, g.Map)
			m := s.Map()
			c := m.Counts()
			check := func(name string, got, want int) {
				if got != want {
					t.Errorf("%s: %s = %d, want %d", g.Map, name, got, want)
				}
			}
			if m.Checksum != g.Checksum {
				t.Errorf("checksum %d, want %d", m.Checksum, g.Checksum)
			}
			check("numclusters", c.Clusters, g.NumClusters)
			check("numareas", c.Areas, g.NumAreas)
			check("numleafs", c.Leafs, g.NumLeafs)
			check("numnodes", c.Nodes, g.NumNodes)
			check("numplanes", c.Planes, g.NumPlanes)
			check("numbrushes", c.Brushes, g.NumBrushes)
			check("numbrushsides", c.BrushSides, g.NumBrushSides)
			check("numleafbrushes", c.LeafBrushes, g.NumLeafBrushes)
			check("numtexinfo", c.TexInfo, g.NumTexInfo)
			check("numareaportals", c.AreaPortals, g.NumAreaPortals)
			check("numcmodels", c.Models, g.NumCModels)
			sum := sha256.Sum256([]byte(m.EntityString()))
			if hex.EncodeToString(sum[:]) != g.EntSHA {
				t.Errorf("entity string sha256 mismatch")
			}
			for i, gc := range g.CModels {
				if i >= m.NumInlineModels() {
					break
				}
				cm := &m.cmodels[i]
				if !testutil.SameVec3(cm.Mins, testutil.Vec3(gc.Mins)) || !testutil.SameVec3(cm.Maxs, testutil.Vec3(gc.Maxs)) ||
					!testutil.SameVec3(cm.Origin, testutil.Vec3(gc.Origin)) || cm.Headnode != gc.Headnode {
					t.Fatalf("cmodel %d: got %+v want %+v", i, *cm, gc)
				}
			}
			rowbytes := (m.NumClusters() + 7) >> 3
			for i := range g.PVS {
				if got := hex.EncodeToString(s.ClusterPVS(i)[:rowbytes]); got != g.PVS[i] {
					t.Fatalf("PVS cluster %d:\n got %s\nwant %s", i, got, g.PVS[i])
				}
			}
			for i := range g.PHS {
				if got := hex.EncodeToString(s.ClusterPHS(i)[:rowbytes]); got != g.PHS[i] {
					t.Fatalf("PHS cluster %d:\n got %s\nwant %s", i, got, g.PHS[i])
				}
			}
			for i, l := range g.Leafs {
				if m.LeafContents(i) != l.Contents || m.LeafCluster(i) != l.Cluster || m.LeafArea(i) != l.Area {
					t.Fatalf("leaf %d: got %d/%d/%d want %+v", i, m.LeafContents(i), m.LeafCluster(i), m.LeafArea(i), l)
				}
			}
		})
	}
}

type jPlane struct {
	Normal   []float64 `json:"normal"`
	Dist     float64   `json:"dist"`
	Type     uint8     `json:"type"`
	SignBits uint8     `json:"signbits"`
}

type jTraceLine struct {
	Q struct {
		Kind     string    `json:"kind"`
		Start    []float64 `json:"start"`
		End      []float64 `json:"end"`
		Mins     []float64 `json:"mins"`
		Maxs     []float64 `json:"maxs"`
		Headnode int32     `json:"headnode"`
		Mask     int32     `json:"mask"`
		Origin   []float64 `json:"origin"`
		Angles   []float64 `json:"angles"`
		P        []float64 `json:"p"`
	} `json:"q"`
	R struct {
		AllSolid   int       `json:"allsolid"`
		StartSolid int       `json:"startsolid"`
		Fraction   float64   `json:"fraction"`
		EndPos     []float64 `json:"endpos"`
		Plane      jPlane    `json:"plane"`
		Surface    struct {
			Name  string `json:"name"`
			Flags int32  `json:"flags"`
			Value int32  `json:"value"`
		} `json:"surface"`
		Contents int32  `json:"contents"`
		Leaf     *int32 `json:"leaf"`
	} `json:"r"`
}

func fmtTrace(tr *shared.Trace) string {
	name := ""
	var flags, value int32
	if tr.Surface != nil {
		name, flags, value = tr.Surface.Name, tr.Surface.Flags, tr.Surface.Value
	}
	return fmt.Sprintf("allsolid=%v startsolid=%v fraction=%s endpos=%s plane={%s %s type=%d sign=%d} surface={%q %d %d} contents=%d",
		tr.AllSolid, tr.StartSolid, testutil.FmtF32(tr.Fraction), testutil.FmtVec3(tr.EndPos),
		testutil.FmtVec3(tr.Plane.Normal), testutil.FmtF32(tr.Plane.Dist), tr.Plane.Type, tr.Plane.SignBits,
		name, flags, value, tr.Contents)
}

func TestGoldenTrace(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(testutil.FixturesDir(), "core", "trace", "*.jsonl"))
	if len(files) == 0 {
		t.Skip("no core/trace fixtures")
	}
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		t.Run(name, func(t *testing.T) {
			s := goldenMap(t, name)
			n := 0
			err := testutil.ReadJSONL(path, func(lineNo int, line []byte) error {
				var g jTraceLine
				if err := json.Unmarshal(line, &g); err != nil {
					return err
				}
				n++
				q := &g.Q
				v := testutil.Vec3
				switch q.Kind {
				case "box", "transformed":
					var tr shared.Trace
					if q.Kind == "box" {
						tr = s.BoxTrace(v(q.Start), v(q.End), v(q.Mins), v(q.Maxs), q.Headnode, q.Mask)
					} else {
						tr = s.TransformedBoxTrace(v(q.Start), v(q.End), v(q.Mins), v(q.Maxs), q.Headnode, q.Mask, v(q.Origin), v(q.Angles))
					}
					r := &g.R
					want := shared.Trace{
						AllSolid: r.AllSolid != 0, StartSolid: r.StartSolid != 0, Fraction: float32(r.Fraction),
						EndPos: v(r.EndPos), Contents: r.Contents,
						Plane:   shared.CPlane{Normal: v(r.Plane.Normal), Dist: float32(r.Plane.Dist), Type: r.Plane.Type, SignBits: r.Plane.SignBits},
						Surface: &shared.CSurface{Name: testutil.Latin1(r.Surface.Name), Flags: r.Surface.Flags, Value: r.Surface.Value},
					}
					ok := tr.AllSolid == want.AllSolid && tr.StartSolid == want.StartSolid &&
						testutil.SameF32(tr.Fraction, want.Fraction) && testutil.SameVec3(tr.EndPos, want.EndPos) &&
						testutil.SameVec3(tr.Plane.Normal, want.Plane.Normal) && testutil.SameF32(tr.Plane.Dist, want.Plane.Dist) &&
						tr.Plane.Type == want.Plane.Type && tr.Plane.SignBits == want.Plane.SignBits &&
						tr.Surface != nil && *tr.Surface == *want.Surface && tr.Contents == want.Contents
					if !ok {
						return fmt.Errorf("query #%d %s\n got  %s\n want %s", n, line[:strings.Index(string(line), `"r":`)], fmtTrace(&tr), fmtTrace(&want))
					}
				case "point":
					if got := s.PointContents(v(q.P), q.Headnode); got != g.R.Contents {
						return fmt.Errorf("query #%d point: got %d want %d (%s)", n, got, g.R.Contents, line)
					}
				case "pointt":
					if got := s.TransformedPointContents(v(q.P), q.Headnode, v(q.Origin), v(q.Angles)); got != g.R.Contents {
						return fmt.Errorf("query #%d pointt: got %d want %d (%s)", n, got, g.R.Contents, line)
					}
				case "leaf":
					if g.R.Leaf == nil {
						return fmt.Errorf("query #%d leaf: missing result", n)
					}
					if got := s.PointLeafnum(v(q.P)); got != *g.R.Leaf {
						return fmt.Errorf("query #%d leaf: got %d want %d (%s)", n, got, *g.R.Leaf, line)
					}
				default:
					return fmt.Errorf("unknown kind %q", q.Kind)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: %d queries bit-exact", name, n)
		})
	}
}
