package app

// ClientKind is the closed vocabulary for application identity. It is separate
// from session.Agent (default/looking) and the user-defined companion role text.
type ClientKind string

const (
	ClientCoding     ClientKind = "coding"
	ClientFigma      ClientKind = "figma"
	ClientMail       ClientKind = "mail"
	ClientSlack      ClientKind = "slack"
	ClientPowerPoint ClientKind = "powerpoint"
	ClientWord       ClientKind = "word"
	ClientExcel      ClientKind = "excel"
	ClientDocker     ClientKind = "docker"
	ClientKubernetes ClientKind = "kubernetes"
)

func ClientKinds() []ClientKind {
	return []ClientKind{ClientCoding, ClientFigma, ClientMail, ClientSlack, ClientPowerPoint, ClientWord, ClientExcel, ClientDocker, ClientKubernetes}
}

// Empty and github were legacy spellings for coding; normalize at input boundaries.
func NormalizeClientKind(role string) ClientKind {
	if role == "" || role == "github" {
		return ClientCoding
	}
	return ClientKind(role)
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

// CompanionType preserves the existing console coding ID. GitHub is coding too.
func (kind ClientKind) CompanionType() string {
	if kind == ClientCoding {
		return "1"
	}
	if !kind.Valid() {
		return "unknown"
	}
	return string(kind.AgentKind())
}

func (a *App) ClientKind() ClientKind { return NormalizeClientKind(a.cfg.ClientRole) }
