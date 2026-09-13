package tiny64

import (
	"flag"
	"fmt"
)

// SetKernalMode selects the active KERNAL profile. Supported values are
// "stock" and "wedge".
func SetKernalMode(mode string) error {
	switch mode {
	case "stock":
		DisableDOSWedge()
	case "wedge":
		EnableDOSWedge()
	default:
		return fmt.Errorf("unknown -kernal %q, want \"stock\" or \"wedge\"", mode)
	}
	return nil
}

// ResolveKernalModeFlags resolves -kernal and -wedge flag combinations.
func ResolveKernalModeFlags(kernal string, wedge bool, explicitKernal, explicitWedge bool) (string, error) {
	if explicitWedge && wedge && explicitKernal && kernal != "wedge" {
		return "", fmt.Errorf("conflicting flags: -wedge cannot be combined with -kernal %q", kernal)
	}
	if wedge {
		return "wedge", nil
	}
	return kernal, nil
}

// ResolveKernalModeFromFlagSet resolves the front-end -kernal/-wedge flags.
func ResolveKernalModeFromFlagSet(fs *flag.FlagSet, kernal string, wedge bool) (string, error) {
	var explicitKernal, explicitWedge bool
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "kernal" {
			explicitKernal = true
		}
		if f.Name == "wedge" {
			explicitWedge = true
		}
	})
	return ResolveKernalModeFlags(kernal, wedge, explicitKernal, explicitWedge)
}
