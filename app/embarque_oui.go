//go:build embarque

package main

import _ "embed"

// archiveKitGz: the kit's files (template/, env.example, LICENSE, app/), made by athanor construire just
// before this build.
//
//go:embed embarque/kit.tar.gz
var archiveKitGz []byte
