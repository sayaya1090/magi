package app

// ClientKind is the closed vocabulary for application identity. It is separate
// from session.Agent (default/looking) and the user-defined companion role text.
type ClientKind string

const (
	ClientCoding     ClientKind = ""
	ClientGitHub     ClientKind = "github"
	ClientFigma      ClientKind = "figma"
	ClientMail       ClientKind = "mail"
	ClientSlack      ClientKind = "slack"
	ClientPowerPoint ClientKind = "powerpoint"
	ClientWord       ClientKind = "word"
	ClientExcel      ClientKind = "excel"
)

func ClientKinds() []ClientKind {
	return []ClientKind{ClientCoding, ClientGitHub, ClientFigma, ClientMail, ClientSlack, ClientPowerPoint, ClientWord, ClientExcel}
}

// Valid deliberately does not infer validity from whether a prompt is nonempty.
func (kind ClientKind) Valid() bool {
	for _, known := range ClientKinds() {
		if kind == known {
			return true
		}
	}
	return false
}
