//go:build unix

package wow

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Running returns the IDs of the running processes whose program is in the
// install at game, such as its World of Warcraft.app.
func Running(game string) ([]int, error) {
	out, err := exec.Command("ps", "-axo", "pid=,comm=").Output()
	if err != nil {
		return nil, err
	}
	return inFolder(string(out), game), nil
}

// inFolder picks from ps's lines ("<pid> <program>") those whose program is
// in folder.
func inFolder(ps, folder string) []int {
	prefix := filepath.Clean(folder) + string(filepath.Separator)
	var pids []int
	for _, line := range strings.Split(ps, "\n") {
		pid, program, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || !strings.HasPrefix(strings.TrimSpace(program), prefix) {
			continue
		}
		if n, err := strconv.Atoi(pid); err == nil {
			pids = append(pids, n)
		}
	}
	return pids
}
