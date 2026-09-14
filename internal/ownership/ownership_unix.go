//go:build unix

package ownership

import (
	"os"
	"syscall"
)

// idsOf extracts uid/gid from the platform stat block. Split out behind a build
// tag because syscall.Stat_t does not exist on every platform Go compiles for,
// and the package must still build for tooling elsewhere.
func idsOf(fi os.FileInfo) (uid, gid int, ok bool) {
	st, k := fi.Sys().(*syscall.Stat_t)
	if !k {
		return 0, 0, false
	}
	return int(st.Uid), int(st.Gid), true
}
