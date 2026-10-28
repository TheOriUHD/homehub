package store

import "strings"

// likeEscaper neutralises LIKE's wildcards in user-supplied text. Every query
// that builds a pattern from a value uses it with an explicit ESCAPE '\', so
// a folder literally named "%" (or "a_b") matches only itself — unescaped,
// deleting the folder "%" matched "%/%", which is every nested file the user
// owns.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// EscapeLike escapes s so LIKE … ESCAPE '\' matches it literally.
func EscapeLike(s string) string { return likeEscaper.Replace(s) }

// likeUnder is the pattern for "anything below path" in a slash-separated
// hierarchy (folders, RBAC resource paths).
func likeUnder(path string) string { return EscapeLike(path) + "/%" }

// likeContains is the pattern for a substring search.
func likeContains(s string) string { return "%" + EscapeLike(s) + "%" }
