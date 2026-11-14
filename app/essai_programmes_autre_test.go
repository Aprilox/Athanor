//go:build !windows

package main

// arreterProgrammesSous: nothing outside Windows (a running program does not keep its folder from removal).
func arreterProgrammesSous(string) {}
