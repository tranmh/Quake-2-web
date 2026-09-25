package crand

import "testing"

// Expected values produced by glibc rand() (gcc 13, x86-64): first 20 values
// and an FNV-1a 64 hash (xor then multiply) over the first 100000 values.
var glibcVectors = []struct {
	seed  uint32
	first []int32
	hash  uint64
}{
	{1, []int32{1804289383, 846930886, 1681692777, 1714636915, 1957747793, 424238335, 719885386, 1649760492, 596516649, 1189641421, 1025202362, 1350490027, 783368690, 1102520059, 2044897763, 1967513926, 1365180540, 1540383426, 304089172, 1303455736}, 0x71b60f618aef8fd3},
	{0, []int32{1804289383, 846930886, 1681692777, 1714636915, 1957747793, 424238335, 719885386, 1649760492, 596516649, 1189641421, 1025202362, 1350490027, 783368690, 1102520059, 2044897763, 1967513926, 1365180540, 1540383426, 304089172, 1303455736}, 0x71b60f618aef8fd3},
	{12345, []int32{383100999, 858300821, 357768173, 455528251, 133005921, 116285904, 591987137, 102557902, 689413528, 585691128, 789708827, 477528897, 471709721, 433228053, 94737806, 738562773, 1390825938, 320971712, 63710857, 1886550712}, 0xfcde2fbaf5e06825},
	{4294967295, []int32{254925627, 1205188300, 366127624, 1401405153, 76053476, 1604170158, 1302235366, 362229243, 334960208, 1882140968, 960816832, 627031785, 1185930294, 1926318319, 973293805, 2114955885, 1626837490, 1438327473, 1589924831, 1644716430}, 0xe10df553ba675ab6},
	{2147483648, []int32{1336741213, 1210407648, 1447044896, 337392383, 82502902, 538660432, 1313908778, 370221063, 344413073, 1896089129, 2044477265, 1711647701, 128787895, 864892100, 985836693, 2134634618, 1642121871, 1452847611, 539426807, 1663660949}, 0xb3b80f52157131d},
	{7, []int32{1045618677, 1863967299, 1272579899, 461085871, 21961325, 1105564443, 2138782586, 68574097, 1291851600, 118852153, 1131251315, 191929321, 1641615331, 1751255526, 1909053865, 351969720, 462792178, 1691535746, 1693715570, 143120736}, 0xea8e0ecdfab2cce2},
}

func TestGlibc(t *testing.T) {
	for _, v := range glibcVectors {
		r := New(v.seed)
		h := uint64(1469598103934665603)
		for i := 0; i < 100000; i++ {
			x := r.Rand()
			if i < len(v.first) && x != v.first[i] {
				t.Fatalf("seed %d value %d = %d, want %d", v.seed, i, x, v.first[i])
			}
			h = (h ^ uint64(uint32(x))) * 1099511628211
		}
		if h != v.hash {
			t.Errorf("seed %d: hash %#x, want %#x", v.seed, h, v.hash)
		}
	}
}

func TestReseed(t *testing.T) {
	r := New(99)
	a := []int32{r.Rand(), r.Rand()}
	r.Srand(99)
	if r.Rand() != a[0] || r.Rand() != a[1] {
		t.Fatal("reseed did not restart the sequence")
	}
}

func TestHelpers(t *testing.T) {
	r := New(1) // 1804289383 & 0x7fff = 0x...
	v := int32(1804289383 & 0x7fff)
	if got := r.Random(); got != v {
		t.Fatalf("Random = %d want %d", got, v)
	}
	r.Srand(1)
	if got, want := r.Frand(), float32(float64(v)*(1.0/32767)); got != want {
		t.Fatalf("Frand = %v want %v", got, want)
	}
	r.Srand(1)
	if got, want := r.Crand(), float32(float64(v)*(2.0/32767)-1); got != want {
		t.Fatalf("Crand = %v want %v", got, want)
	}
	r.Srand(1)
	g := r.GRandom()
	if g != float32(v)/32767 {
		t.Fatalf("GRandom = %v", g)
	}
	r.Srand(1)
	if c := r.GCrandom(); c != 2.0*(float64(g)-0.5) {
		t.Fatalf("GCrandom = %v", c)
	}
	for i := 0; i < 10000; i++ {
		if f := r.GRandom(); f < 0 || f > 1 {
			t.Fatalf("GRandom out of range %v", f)
		}
	}
}

func BenchmarkRand(b *testing.B) {
	r := New(1)
	for i := 0; i < b.N; i++ {
		r.Rand()
	}
}
