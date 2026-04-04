package config

import "time"

type GlobalConfig struct {
	LLM      *LLMConfig
	Defaults *RepoDefaults
	Alerts   []*AlertConfig
	Repos    []*RepoEntry
}

type HookEntry struct {
	Command string
	Timeout string
	Dir     string
	Env     map[string]string
}

type HooksConfig struct {
	PreCommit  []*HookEntry
	PostCommit []*HookEntry
	PostPull   []*HookEntry
	PrePush    []*HookEntry
	PostSync   []*HookEntry
}

type HoldoutWindowEntry struct {
	Start string
	End   string
}

type RepoDefaults struct {
	PollInterval        string
	Branch              string
	Remote              string
	CommitMessagePrompt string
	HoldoutWindows      []*HoldoutWindowEntry
	Hooks               *HooksConfig
}

type RepoEntry struct {
	Path                string
	Name                string
	PollInterval        string
	Branch              string
	Remote              string
	CommitMessagePrompt string
	HoldoutWindows      []*HoldoutWindowEntry
	Hooks               *HooksConfig
}

type LLMConfig struct {
	Endpoint  string
	Model     string
	APIKeyEnv string
	MaxTokens int
}

type AlertConfig struct {
	Type       string
	Mattermost *MattermostConfig
}

type MattermostConfig struct {
	URL          string `dd:",+required"`
	Token        string
	TokenEnv     string
	ChannelID    string `dd:",+required"`
	TriggerWords []string
	AllowedUsers []string
}

type RepoLocalConfig struct {
	Name                string
	PollInterval        string
	Branch              string
	Remote              string
	CommitMessagePrompt string
	HoldoutWindows      []*HoldoutWindowEntry
	Hooks               *HooksConfig
}

type ResolvedHook struct {
	Command string
	Timeout time.Duration
	Dir     string
	Env     map[string]string
}

type ResolvedHooks struct {
	PreCommit  []*ResolvedHook
	PostCommit []*ResolvedHook
	PostPull   []*ResolvedHook
	PrePush    []*ResolvedHook
	PostSync   []*ResolvedHook
}

type ResolvedHoldoutWindow struct {
	StartMinute int
	EndMinute   int
}

type ResolvedRepo struct {
	Path                string
	Name                string
	ExplicitName        bool
	PollInterval        time.Duration
	Branch              string
	Remote              string
	CommitMessagePrompt string
	HoldoutWindows      []*ResolvedHoldoutWindow
	Hooks               *ResolvedHooks
}

const DefaultCommitMessagePrompt = "Summarize the following git diff as a concise commit message. Use imperative mood. Be specific about what changed."

func defaultGlobalConfig() *GlobalConfig {
	return &GlobalConfig{
		Defaults: &RepoDefaults{
			PollInterval:        "30s",
			Branch:              "main",
			Remote:              "origin",
			CommitMessagePrompt: DefaultCommitMessagePrompt,
		},
	}
}
