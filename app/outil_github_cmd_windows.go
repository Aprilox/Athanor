//go:build windows

package main

import "io"

// Native Windows keeps system/github.sh (its locks are the Git Bash ones); only its Python is here.
func commandeGitHub(args []string, w io.Writer) (int, bool) { return 0, false }
