package skill

// ManageRequest is the stable skill_manage input contract.
type ManageRequest struct {
	Action   string  `json:"action"`
	Skill    string  `json:"skill,omitempty"`
	SkillRef string  `json:"skill_ref,omitempty"`
	Key      string  `json:"key,omitempty"`
	Value    *string `json:"value,omitempty"`
	Source   string  `json:"source,omitempty"`
	Digest   string  `json:"digest,omitempty"`
	Purge    bool    `json:"purge,omitempty"`
}
