package navbuild

import (
	"math"
	"sort"

	"quake2web/server/internal/bsp"
)

// Brush geometry in float64: windings of brush sides, as qbsp computes them
// (BaseWindingForPlane clipped by the other sides of the brush).

type dvec [3]float64

type dplane struct {
	n dvec
	d float64
}

type winding []dvec

func (a dvec) dot(b dvec) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func (a dvec) sub(b dvec) dvec    { return dvec{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func (a dvec) add(b dvec) dvec    { return dvec{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func (a dvec) scale(s float64) dvec {
	return dvec{a[0] * s, a[1] * s, a[2] * s}
}
func (a dvec) cross(b dvec) dvec {
	return dvec{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}
func (a dvec) length() float64 { return math.Sqrt(a.dot(a)) }

// bogusRange is half the size of the base winding (larger than any map).
const bogusRange = 1 << 17

// baseWinding returns a huge square on the plane.
// C (qbsp): common/polylib.c BaseWindingForPlane
func baseWinding(p dplane) winding {
	// find the major axis
	maxv, x := -1.0, -1
	for i := 0; i < 3; i++ {
		if v := math.Abs(p.n[i]); v > maxv {
			maxv, x = v, i
		}
	}
	var vup dvec
	if x == 2 {
		vup[0] = 1
	} else {
		vup[2] = 1
	}
	v := vup.dot(p.n)
	vup = vup.sub(p.n.scale(v))
	vup = vup.scale(1 / vup.length())
	org := p.n.scale(p.d)
	vright := vup.cross(p.n)
	vup = vup.scale(bogusRange)
	vright = vright.scale(bogusRange)
	return winding{
		org.sub(vright).add(vup),
		org.add(vright).add(vup),
		org.add(vright).sub(vup),
		org.sub(vright).sub(vup),
	}
}

// clipBack keeps the part of w behind p (dot(x, n) - d <= eps).
// C (qbsp): common/polylib.c ClipWindingEpsilon (back side)
func (w winding) clipBack(p dplane, eps float64) winding {
	n := len(w)
	if n == 0 {
		return nil
	}
	dists := make([]float64, n)
	sides := make([]int8, n) // 1 front, -1 back, 0 on
	front := 0
	for i, pt := range w {
		d := pt.dot(p.n) - p.d
		dists[i] = d
		switch {
		case d > eps:
			sides[i] = 1
			front++
		case d < -eps:
			sides[i] = -1
		}
	}
	if front == 0 {
		return w
	}
	out := make(winding, 0, n+2)
	for i := 0; i < n; i++ {
		p1 := w[i]
		if sides[i] <= 0 {
			out = append(out, p1)
		}
		j := (i + 1) % n
		if sides[i] == 0 || sides[j] == 0 || sides[i] == sides[j] {
			continue
		}
		// split the edge
		p2 := w[j]
		t := dists[i] / (dists[i] - dists[j])
		var mid dvec
		for k := 0; k < 3; k++ {
			// avoid round off error when possible
			switch {
			case p.n[k] == 1:
				mid[k] = p.d
			case p.n[k] == -1:
				mid[k] = -p.d
			default:
				mid[k] = p1[k] + t*(p2[k]-p1[k])
			}
		}
		out = append(out, mid)
	}
	if len(out) < 3 {
		return nil
	}
	return out
}

// area returns the winding's area.
func (w winding) area() float64 {
	var total float64
	for i := 2; i < len(w); i++ {
		total += w[i-1].sub(w[0]).cross(w[i].sub(w[0])).length() * 0.5
	}
	return total
}

// centroid returns the average of the vertices.
func (w winding) centroid() dvec {
	var c dvec
	for _, p := range w {
		c = c.add(p)
	}
	return c.scale(1 / float64(len(w)))
}

// insideXY reports whether (x, y) lies in the winding's projection onto the
// xy plane (boundary included, within eps). The winding must be convex and
// not vertical.
func (w winding) insideXY(x, y, eps float64) bool {
	n := len(w)
	sign := 0.0
	for i := 0; i < n; i++ {
		a, b := w[i], w[(i+1)%n]
		ex, ey := b[0]-a[0], b[1]-a[1]
		l := math.Hypot(ex, ey)
		if l < 1e-9 {
			continue
		}
		c := (ex*(y-a[1]) - ey*(x-a[0])) / l // signed distance to the edge line
		if sign == 0 {
			// orientation from the polygon's signed area
			sign = 1
			if w.signedAreaXY() < 0 {
				sign = -1
			}
		}
		if c*sign < -eps {
			return false
		}
	}
	return true
}

func (w winding) signedAreaXY() float64 {
	var a float64
	for i := range w {
		p, q := w[i], w[(i+1)%len(w)]
		a += p[0]*q[1] - q[0]*p[1]
	}
	return a / 2
}

// bounds returns the winding's bounding box.
func (w winding) bounds() (min, max dvec) {
	min = dvec{math.Inf(1), math.Inf(1), math.Inf(1)}
	max = dvec{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, p := range w {
		for k := 0; k < 3; k++ {
			min[k] = math.Min(min[k], p[k])
			max[k] = math.Max(max[k], p[k])
		}
	}
	return min, max
}

// zAt returns the height of the plane at (x, y) (normal.z must be > 0).
func (p dplane) zAt(x, y float64) float64 { return (p.d - p.n[0]*x - p.n[1]*y) / p.n[2] }

func planeOf(f *bsp.File, num uint16) dplane {
	pl := f.Planes[num]
	return dplane{n: dvec{float64(pl.Normal[0]), float64(pl.Normal[1]), float64(pl.Normal[2])}, d: float64(pl.Dist)}
}

// face is one side of a brush with its winding.
type face struct {
	brush, side int
	plane       dplane
	w           winding
}

// brushFaces returns the windings of brush b's sides that satisfy keep
// (tested on the side's plane), skipping degenerate ones (area < minArea).
func brushFaces(f *bsp.File, b int, keep func(dplane) bool, minArea float64) []face {
	br := f.Brushes[b]
	if br.FirstSide < 0 || br.NumSides <= 0 || int(br.FirstSide+br.NumSides) > len(f.BrushSides) {
		return nil
	}
	var out []face
	for s := br.FirstSide; s < br.FirstSide+br.NumSides; s++ {
		pn := f.BrushSides[s].PlaneNum
		if int(pn) >= len(f.Planes) {
			continue
		}
		pl := planeOf(f, pn)
		if !keep(pl) {
			continue
		}
		w := baseWinding(pl)
		for t := br.FirstSide; t < br.FirstSide+br.NumSides && w != nil; t++ {
			if t == s {
				continue
			}
			tn := f.BrushSides[t].PlaneNum
			if int(tn) >= len(f.Planes) || tn == pn {
				continue
			}
			w = w.clipBack(planeOf(f, tn), 0.01)
		}
		if w == nil || w.area() < minArea {
			continue
		}
		out = append(out, face{brush: b, side: int(s), plane: pl, w: w})
	}
	return out
}

// modelBrushes returns the brushes reachable from a model's headnode
// (sorted, unique).
func modelBrushes(f *bsp.File, headnode int32) []int {
	seen := map[int]bool{}
	var walk func(n int32, depth int)
	walk = func(n int32, depth int) {
		if depth > 1024 {
			return
		}
		if n < 0 {
			l := int(-1 - n)
			if l >= len(f.Leafs) {
				return
			}
			lf := f.Leafs[l]
			for i := int(lf.FirstLeafBrush); i < int(lf.FirstLeafBrush)+int(lf.NumLeafBrushes) && i < len(f.LeafBrushes); i++ {
				if b := int(f.LeafBrushes[i]); b < len(f.Brushes) {
					seen[b] = true
				}
			}
			return
		}
		if int(n) >= len(f.Nodes) {
			return
		}
		nd := f.Nodes[n]
		walk(nd.Children[0], depth+1)
		walk(nd.Children[1], depth+1)
	}
	walk(headnode, 0)
	out := make([]int, 0, len(seen))
	for b := range seen {
		out = append(out, b)
	}
	sort.Ints(out)
	return out
}

// gridPoints returns the points of the world-aligned grid (spacing g, with
// the given offset) that fall inside the winding's xy projection.
func gridPoints(w winding, g, offX, offY float64) [][2]float64 {
	min, max := w.bounds()
	var out [][2]float64
	for x := math.Ceil((min[0]-offX)/g)*g + offX; x <= max[0]; x += g {
		for y := math.Ceil((min[1]-offY)/g)*g + offY; y <= max[1]; y += g {
			if w.insideXY(x, y, 0.01) {
				out = append(out, [2]float64{x, y})
			}
		}
	}
	return out
}

// segmentBox reports whether the segment a-b passes through the box
// (slab test).
func segmentBox(a, b, min, max dvec) bool {
	t0, t1 := 0.0, 1.0
	for k := 0; k < 3; k++ {
		d := b[k] - a[k]
		if math.Abs(d) < 1e-12 {
			if a[k] < min[k] || a[k] > max[k] {
				return false
			}
			continue
		}
		u0, u1 := (min[k]-a[k])/d, (max[k]-a[k])/d
		if u0 > u1 {
			u0, u1 = u1, u0
		}
		t0, t1 = math.Max(t0, u0), math.Min(t1, u1)
		if t0 > t1 {
			return false
		}
	}
	return true
}
