package appbridge

import "strings"

// CompanionEnv keeps provider credentials but removes application/helper secrets.
// Coding companions have shell access; API tokens belong only to the helper.
func CompanionEnv(from []string, configDir string) []string {
	out := make([]string, 0, len(from)+1)
	for _, entry := range from {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "MAGI_CONFIG_DIR", "MAGI_CLIENT_ROLE", "MAGI_FLEET_LISTEN",
			"GITHUB_TOKEN", "GH_TOKEN", "MAGI_GITHUB_TOKEN", "MAGI_FIGMA_TOKEN",
			"MAGI_MAIL_PASSWORD", "MAGI_MAIL_TOKEN", "MAGI_SLACK_TOKEN",
			"MAGI_OPERATIONS_TOKEN", "KUBECONFIG", "DOCKER_CERT_PATH", "DOCKER_CONFIG",
			"SLACK_BOT_TOKEN", "SLACK_APP_TOKEN", "SLACK_SIGNING_SECRET":
			continue
		}
		out = append(out, entry)
	}
	return append(out, "MAGI_CONFIG_DIR="+configDir)
}
