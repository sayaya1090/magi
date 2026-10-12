package app

// AgentKind describes the work, not the host application or current task phase.
type AgentKind string

const (
	AgentCoding        AgentKind = "coding"
	AgentDesign        AgentKind = "design"
	AgentCommunication AgentKind = "communication"
	AgentDocument      AgentKind = "document"
	AgentPresentation  AgentKind = "presentation"
	AgentSpreadsheet   AgentKind = "spreadsheet"
)

func AgentKinds() []AgentKind {
	return []AgentKind{AgentCoding, AgentDesign, AgentCommunication, AgentDocument, AgentPresentation, AgentSpreadsheet}
}

// Application-specific tool gates still use the client identity. Sharing a
// communication kind never grants mail access to Slack or vice versa.
func (client ClientKind) AgentKind() AgentKind {
	switch client {
	case ClientCoding:
		return AgentCoding
	case ClientFigma:
		return AgentDesign
	case ClientMail, ClientSlack:
		return AgentCommunication
	case ClientWord:
		return AgentDocument
	case ClientPowerPoint:
		return AgentPresentation
	case ClientExcel:
		return AgentSpreadsheet
	}
	return ""
}
