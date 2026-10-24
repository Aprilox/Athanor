//go:build !linux && !darwin

package main

// Native Windows: the MCP servers run without a cage (outil_extensions.go writes them so).
func lancerMCPCage(args []string) int { return pasIci("mcp-cage") }
