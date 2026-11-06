package handler

import (
	"fmt"
	"strings"
)

// Any private header name may carry a credential. A fixed list such as
// Authorization/X-Api-Key cannot safely classify a user-defined header map.
func transformHeaderSecrets(headers, stored map[string]interface{}, restoring bool) error {
	seen := map[string]bool{}
	for name, value := range headers {
		key := strings.ToLower(strings.TrimSpace(name))
		if restoring && seen[key] {
			return fmt.Errorf("Duplicate request header name")
		}
		seen[key] = true
		text, ok := value.(string)
		if !ok {
			continue
		}
		if !restoring {
			if text != "" {
				headers[name] = maskedSecret
			}
			continue
		}
		if text != maskedSecret {
			continue
		}
		found := ""
		count := 0
		for oldName, oldValue := range stored {
			if strings.EqualFold(strings.TrimSpace(oldName), strings.TrimSpace(name)) {
				if oldText, ok := oldValue.(string); ok {
					found = oldText
					count++
				}
			}
		}
		if count != 1 || found == "" || found == maskedSecret {
			return fmt.Errorf("No unique saved header credential exists; enter a new value")
		}
		headers[name] = found
	}
	return nil
}
