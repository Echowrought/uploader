package saves

import "strings"

const (
	heroPrefix                = "HERO_"
	accountPrefix             = "ACCOUNT_"
	accountVerificationPrefix = "ACCOUNT_VERIFY"
)

// Essential files must have settled versions when present. This list does not
// require every category to exist; validateEssential checks minimum presence.
var essentialPrefixes = [...]string{
	heroPrefix,
	accountPrefix,
	"ENTITLEMENTS_",
	"SETTINGS_",
	"LEGACY_",
}

func essential(name string) bool {
	upper := strings.ToUpper(name)
	for _, prefix := range essentialPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}
