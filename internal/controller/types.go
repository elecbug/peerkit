package controller

type RunOptions struct {
	ProjectRoot         string
	OutputDir           string
	Image               string
	NoBuild             bool
	Keep                bool
	ReadyTimeoutSeconds int
}

type RunMetadata struct {
	RunID        string         `yaml:"run_id" json:"run_id"`
	ProjectName  string         `yaml:"project_name" json:"project_name"`
	ComposeFile  string         `yaml:"compose_file" json:"compose_file"`
	ScenarioFile string         `yaml:"scenario_file" json:"scenario_file"`
	ControlPorts map[string]int `yaml:"control_ports" json:"control_ports"`
}
