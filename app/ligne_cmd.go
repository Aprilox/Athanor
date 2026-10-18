package main

import "strings"

// entreGuillemetsCmd: the arguments of a "cmd.exe /s /c …" line. cmd splits a line on & | < > ^ ( )
// outside quotes, and the usual quoting (EscapeArg) only adds quotes for spaces: an argument holding
// one of them (a folder "R&D", a value given to a .bat) is put between quotes, where cmd leaves it whole.
// args are the raw arguments, echappes the same ones already quoted for CreateProcess.
func entreGuillemetsCmd(args, echappes []string) []string {
	out := make([]string, len(echappes))
	for i, e := range echappes {
		if !strings.HasPrefix(e, `"`) && strings.ContainsAny(args[i], "&|<>^()") {
			n := len(e) - len(strings.TrimRight(e, `\`)) // a final backslash would escape the closing quote: doubled
			e = `"` + e + strings.Repeat(`\`, n) + `"`
		}
		out[i] = e
	}
	return out
}
