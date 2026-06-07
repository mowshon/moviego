package ffmpeg

import "strings"

// SafeInputPath guards a filename that begins with '-' from being parsed as a
// flag by prefixing "./", mirroring the FFmpeg filename-safety rule. It applies
// equally to input and output filenames (FFmpeg parses both positionally), so
// the encoders use it for the output path too.
func SafeInputPath(path string) string {
	if strings.HasPrefix(path, "-") {
		return "./" + path
	}
	return path
}

// filterValueEscaper backslash-escapes the characters special to a filtergraph
// option value. It covers both the option level (\ ' :) and the graph level
// ([ ] , ;); FFmpeg args are passed directly (no shell), so shell-level
// escaping is not needed. Backslash is listed first so the escapes it inserts
// are not themselves re-escaped (strings.Replacer scans once, left to right).
var filterValueEscaper = strings.NewReplacer(
	`\`, `\\`,
	`'`, `\'`,
	`:`, `\:`,
	`[`, `\[`,
	`]`, `\]`,
	`,`, `\,`,
	`;`, `\;`,
)

// escapeFilterValue escapes a string so it is safe as a filtergraph option
// value (e.g. the file= path of lut3d). It does not add the leading "./" guard
// — that is for positional CLI filenames, not filter values.
func escapeFilterValue(s string) string {
	return filterValueEscaper.Replace(s)
}
