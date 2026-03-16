package config

import "time"

type GlobalConfig struct {
	LLM      *LLMConfig    `yaml:"llm"`
	Defaults *RepoDefaults `yaml:"defaults"`
	Alerts   []*AlertConfig `yaml:"alerts"`
	Repos    []*RepoEntry  `yaml:"repos"`
}

type RepoDefaults struct {
	PollInterval       string `yaml:"poll_interval"`
	Branch             string `yaml:"branch"`
	Remote             string `yaml:"remote"`
	CommitMessagePrompt string `yaml:"commit_message_prompt"`
}

type RepoEntry struct {
	Path                string `yaml:"path"`
	Name                string `yaml:"name"`
	PollInterval        string `yaml:"poll_interval"`
	Branch              string `yaml:"branch"`
	Remote              string `yaml:"remote"`
	CommitMessagePrompt string `yaml:"commit_message_prompt"`
}

type LLMConfig struct {
	Endpoint  string `yaml:"endpoint"`
	Model     string `yaml:"model"`
	APIKeyEnv string `yaml:"api_key_env"`
	MaxTokens int    `yaml:"max_tokens"`
}

type AlertConfig struct {
	Type string `yaml:"type"`
}

type RepoLocalConfig struct {
	Name                string `yaml:"name"`
	PollInterval        string `yaml:"poll_interval"`
	Branch              string `yaml:"branch"`
	Remote              string `yaml:"remote"`
	CommitMessagePrompt string `yaml:"commit_message_prompt"`
}

type ResolvedRepo struct {
	Path                string
	Name                string
	PollInterval        time.Duration
	Branch              string
	Remote              string
	CommitMessagePrompt string
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
