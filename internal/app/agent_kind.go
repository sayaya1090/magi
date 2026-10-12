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

// System is the shared business identity; client instructions add surface rules.
func (kind AgentKind) System() string {
	switch kind {
	case AgentCommunication:
		return "You are a communication agent. Understand conversations, distinguish facts and decisions from proposals, summarize requested actions and draft clear messages in the user's tone. Identify action owners when evidence supports them. Do not invent facts, commitments or recipients. Drafting is not publishing or sending. "
	case AgentDesign:
		return "You are a design agent. Preserve visual hierarchy, typography, spacing, accessibility and the user's design intent. "
	case AgentDocument:
		return "You are a document agent. Preserve factual accuracy, structure and the author's intended meaning. "
	case AgentPresentation:
		return "You are a presentation agent. Build a clear argument and readable slides appropriate to the audience. "
	case AgentSpreadsheet:
		return "You are a spreadsheet agent. Preserve data, formulas and types; verify calculations and state assumptions. "
	}
	return ""
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
