package operator

import (
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

// errBadTreeCursor is a tree cursor that names no position.
var errBadTreeCursor = errors.New("tree cursor names no position")

// encodeTreeCursor is the opaque cursor of a scoped tree read's next page (issue #1424): the position of the last row read.
func encodeTreeCursor(after api.TreePosition) string {
	raw, err := json.Marshal(after)
	if err != nil {
		panic(err) // unreachable: a struct of two integers always marshals
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeTreeCursor reads a cursor encodeTreeCursor wrote. No row forks at zero or has a zero id, so a cursor missing either names no
// position and is rejected rather than read from the start.
func decodeTreeCursor(token string) (api.TreePosition, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return api.TreePosition{}, err
	}
	var after api.TreePosition
	if err := json.Unmarshal(raw, &after); err != nil {
		return api.TreePosition{}, err
	}
	if after.ForkTimeNs <= 0 || after.ID <= 0 {
		return api.TreePosition{}, errBadTreeCursor
	}
	return after, nil
}
