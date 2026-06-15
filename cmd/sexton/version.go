package main

import "github.com/michaelquigley/push/build"

func init() {
	// sexton has no tagged release yet; advertise the dev base as v0.1.x for
	// unstamped developer builds.
	build.DevVersion = "v0.1.x"
}
