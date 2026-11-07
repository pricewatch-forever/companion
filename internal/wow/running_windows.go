package wow

import "errors"

// Running returns the IDs of the running processes whose program is in the
// install at game. It isn't done on Windows yet.
func Running(game string) ([]int, error) {
	return nil, errors.New("finding the game's processes isn't done on Windows yet")
}
