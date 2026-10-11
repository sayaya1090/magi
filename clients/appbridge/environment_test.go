package appbridge

import (
	"reflect"
	"testing"
)

func TestCompanionEnvironmentSeparatesHelperCredentials(t *testing.T) {
	input := []string{"PATH=/bin", "GITHUB_TOKEN=secret", "GH_TOKEN=secret", "MAGI_MAIL_PASSWORD=secret", "SLACK_BOT_TOKEN=secret", "SLACK_APP_TOKEN=secret", "MAGI_FIGMA_TOKEN=secret", "MAGI_CLIENT_ROLE=mail", "MAGI_CONFIG_DIR=old", "MAGI_FLEET_LISTEN=remote", "OPENAI_API_KEY=provider"}
	want := []string{"PATH=/bin", "OPENAI_API_KEY=provider", "MAGI_CONFIG_DIR=new"}
	if got := CompanionEnv(input, "new"); !reflect.DeepEqual(got, want) {
		t.Fatalf("companion environment mismatch: %v", got)
	}
}
