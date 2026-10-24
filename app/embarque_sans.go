//go:build !embarque

package main

// archiveKitGz: none in a plain build (go build, go test): the executable needs a kit folder around it,
// as before 2.26 (athanor construire and the releases build with the embarque tag).
var archiveKitGz []byte
