package app

import (
	"strings"
	"testing"
)

func TestAgentKindIsIndependentOfClientToolBoundary(t *testing.T) {
	want := map[ClientKind]AgentKind{ClientCoding: AgentCoding, ClientFigma: AgentDesign, ClientMail: AgentCommunication, ClientSlack: AgentCommunication, ClientWord: AgentDocument, ClientPowerPoint: AgentPresentation, ClientExcel: AgentSpreadsheet}
	for client, kind := range want {
		if client.AgentKind() != kind {
			t.Fatalf("%s maps to %s", client, client.AgentKind())
		}
	}
	if ClientKind("invalid").AgentKind() != "" {
		t.Fatal("unknown client gained a business kind")
	}
	if ClientRoleAllows("mail", "mcp__slack__read") || ClientRoleAllows("slack", "mcp__mail__read") {
		t.Fatal("communication grouping widened tool permissions")
	}
	if NormalizeClientKind("github").AgentKind() != AgentCoding {
		t.Fatal("GitHub must remain a coding client")
	}
}

func TestCommunicationPromptIsSharedWithoutRemovingClientRules(t *testing.T) {
	shared := AgentCommunication.System()
	if shared == "" {
		t.Fatal("missing communication identity")
	}
	for _, client := range []string{"mail", "slack"} {
		if !strings.Contains(ClientRoleSystem(client), shared) {
			t.Fatalf("%s omits shared identity", client)
		}
	}
	if !strings.Contains(ClientRoleSystem("mail"), "user-confirmed") || !strings.Contains(ClientRoleSystem("slack"), "channel and thread") {
		t.Fatal("client-specific constraints lost")
	}
}
