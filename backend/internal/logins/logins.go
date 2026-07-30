// Package logins ports mobile/app/.../util/Logins.kt.
package logins

import "strings"

// PiscineLoginFromName: piscine CA users are named like
// "[PISCINE] 249 lgauvrea" and carry no ft_login/ft_id — the trailing
// token is the intra login. Returns "" when the name isn't a piscine entry
// or has no usable login.
func PiscineLoginFromName(fullName string) string {
	if !strings.Contains(strings.ToLower(fullName), "[piscine]") {
		return ""
	}
	fields := strings.Fields(strings.TrimSpace(fullName))
	if len(fields) == 0 {
		return ""
	}
	last := fields[len(fields)-1]
	if last == "" {
		return ""
	}
	hasLetter := false
	for _, c := range last {
		isLoginChar := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
		if !isLoginChar {
			return ""
		}
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			hasLetter = true
		}
	}
	if !hasLetter {
		return ""
	}
	return strings.ToLower(last)
}
