package exact

// groupBy buckets items 0..n-1 by key in a single O(n) expected pass, using
// an open-addressing table instead of sorting or a Go map.
//
// hash maps an item to a well-distributed 64-bit value (digests already are;
// sizes go through mix). eq decides true key equality, so a slot or hash
// collision can only cost a probe, never a wrong group.
//
// It returns every bucket with at least two items. Items inside a bucket are
// in ascending index order and buckets are ordered by their first item, so
// path-ordered input gives path-ordered output with no sorting.
func groupBy(n int, hash func(i int) uint64, eq func(a, b int) bool) [][]int32 {
	if n < 2 {
		return nil
	}
	size := 1
	for size < 2*n {
		size <<= 1
	}
	mask := uint64(size - 1)
	table := make([]int32, size) // slot -> bucket leader index; -1 = empty
	for i := range table {
		table[i] = -1
	}
	next := make([]int32, n) // chain of items sharing a leader, in index order
	tail := make([]int32, n) // leader -> last item of its chain
	count := make([]int32, n)
	leader := make([]int32, 0, n)

	for i := 0; i < n; i++ {
		slot := hash(i) & mask
		for {
			j := table[slot]
			if j < 0 {
				table[slot] = int32(i)
				next[i], tail[i], count[i] = -1, int32(i), 1
				leader = append(leader, int32(i))
				break
			}
			if eq(int(j), i) {
				next[tail[j]] = int32(i)
				next[i] = -1
				tail[j] = int32(i)
				count[j]++
				break
			}
			slot = (slot + 1) & mask
		}
	}

	var out [][]int32
	for _, l := range leader { // leaders were appended in index order
		if count[l] < 2 {
			continue
		}
		g := make([]int32, 0, count[l])
		for m := l; m >= 0; m = next[m] {
			g = append(g, m)
		}
		out = append(out, g)
	}
	return out
}

// mix is a 64-bit finaliser (splitmix64) for keys that are not already
// uniformly distributed, such as file sizes.
func mix(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}
