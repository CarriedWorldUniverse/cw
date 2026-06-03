package auth

import (
	"github.com/CarriedWorldUniverse/cwb-client/client"
	"github.com/CarriedWorldUniverse/cw/internal/cmdutil"
	"github.com/CarriedWorldUniverse/cw/internal/config"
)

// session delegates to cmdutil.Session (shared with the repo/pr command groups).
func session(gf *GlobalFlags) (*client.Client, config.Context, string, error) {
	return cmdutil.Session(gf)
}
