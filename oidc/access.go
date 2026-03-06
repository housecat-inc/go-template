package oidc

import "strings"

func CheckAccess(email, allowedDomain, allowedEmails string) bool {
	if allowedDomain == "" && allowedEmails == "" {
		return true
	}

	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false
	}

	if allowedDomain != "" {
		domain := strings.ToLower(strings.TrimSpace(allowedDomain))
		if strings.HasSuffix(email, "@"+domain) {
			return true
		}
	}

	if allowedEmails != "" {
		for _, allowed := range strings.Split(allowedEmails, ",") {
			if strings.ToLower(strings.TrimSpace(allowed)) == email {
				return true
			}
		}
	}

	return false
}
