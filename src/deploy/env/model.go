package deploy

type EnvVersion struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Error     string `json:"error"`
}

type SshKey struct {
	PublicKey string `json:"publicKey"`
	Error     string `json:"error"`
}

type GitRepo struct {
	URL    string   `json:"url"`
	Branch []string `json:"branch"`
}
